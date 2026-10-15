// Package apipolicy stores API controls separately from source-file metadata.
package apipolicy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Rate struct {
	Enabled       bool `json:"enabled"`
	Reads         int  `json:"reads"`
	Writes        int  `json:"writes"`
	WindowSeconds int  `json:"window_seconds"`
}
type Policy struct {
	Rate            Rate     `json:"rate"`
	DefaultPageSize int      `json:"default_page_size"`
	MaxPageSize     int      `json:"max_page_size"`
	Origins         []string `json:"origins,omitempty"`
}
type Collection struct {
	Rate            Rate `json:"rate"`
	DefaultPageSize int  `json:"default_page_size"`
	MaxPageSize     int  `json:"max_page_size"`
}
type bucket struct {
	Count int
	Until time.Time
}
type Manager struct {
	db          *sql.DB
	proxies     []netip.Prefix
	mu          sync.Mutex
	global      Policy
	collections map[string]Collection
	counters    map[string]bucket
	now         func() time.Time
	maxBuckets  int
	sweep       time.Time
}

const globalKey = "api_policy_v1"
const collectionPrefix = "api_collection_v1:"

func DefaultRate() Rate              { return Rate{Reads: 120, Writes: 30, WindowSeconds: 60} }
func Defaults() Policy               { return Policy{Rate: DefaultRate(), MaxPageSize: 100} }
func CollectionDefaults() Collection { return Collection{Rate: DefaultRate()} }
func New(db *sql.DB, trustedProxies ...string) (*Manager, error) {
	proxies, err := ParseProxies(trustedProxies)
	if err != nil {
		return nil, err
	}
	m := &Manager{db: db, proxies: proxies, global: Defaults(), collections: map[string]Collection{}, counters: map[string]bucket{}, now: time.Now, maxBuckets: 20000}
	rows, err := db.Query("SELECT key,value FROM settings WHERE key=? OR substr(key,1,?)=?", globalKey, len(collectionPrefix), collectionPrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if key == globalKey {
			p := Defaults()
			if err = json.Unmarshal([]byte(value), &p); err == nil {
				p, err = Validate(p)
			}
			if err != nil {
				return nil, fmt.Errorf("API settings: %w", err)
			}
			m.global = p
		} else {
			p := CollectionDefaults()
			if err = json.Unmarshal([]byte(value), &p); err == nil {
				err = ValidateCollection(p)
			}
			if err != nil {
				return nil, fmt.Errorf("collection API settings: %w", err)
			}
			m.collections[strings.TrimPrefix(key, collectionPrefix)] = p
		}
	}
	return m, rows.Err()
}
func validateRate(r Rate) error {
	if r.Reads < 1 || r.Reads > 1000000 || r.Writes < 1 || r.Writes > 1000000 || r.WindowSeconds < 1 || r.WindowSeconds > 3600 {
		return fmt.Errorf("request budgets must be 1–1000000 and the window 1–3600 seconds")
	}
	return nil
}
func Validate(p Policy) (Policy, error) {
	if err := validateRate(p.Rate); err != nil {
		return p, err
	}
	if p.MaxPageSize < 1 || p.MaxPageSize > 100 || p.DefaultPageSize < 0 || p.DefaultPageSize > p.MaxPageSize {
		return p, fmt.Errorf("maximum rows must be 1–100; default rows must be 0–maximum")
	}
	if len(p.Origins) > 32 {
		return p, fmt.Errorf("use at most 32 allowed origins")
	}
	origins := []string{}
	seen := map[string]bool{}
	for _, origin := range p.Origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if len(origin) > 2048 {
			return p, fmt.Errorf("origin is too long")
		}
		if origin != "*" {
			u, e := url.Parse(origin)
			if e != nil || u == nil || !(u.Scheme == "https" || u.Scheme == "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(origin, "\r\n\\") {
				return p, fmt.Errorf("use origins such as https://example.com, without paths or credentials")
			}
			origin = strings.ToLower(u.Scheme + "://" + u.Host)
		}
		if !seen[origin] {
			seen[origin] = true
			origins = append(origins, origin)
		}
	}
	if seen["*"] && len(origins) > 1 {
		return p, fmt.Errorf("use * alone or list individual origins")
	}
	p.Origins = origins
	return p, nil
}
func ValidateCollection(p Collection) error {
	if err := validateRate(p.Rate); err != nil {
		return err
	}
	if p.DefaultPageSize < 0 || p.DefaultPageSize > 100 || p.MaxPageSize < 0 || p.MaxPageSize > 100 || (p.MaxPageSize > 0 && p.DefaultPageSize > p.MaxPageSize) {
		return fmt.Errorf("collection page sizes must be 0–100; default cannot exceed its maximum")
	}
	return nil
}
func (m *Manager) Global() Policy {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.global
	p.Origins = append([]string(nil), p.Origins...)
	return p
}
func (m *Manager) Collection(id string) Collection {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.collections[id]; ok {
		return p
	}
	return CollectionDefaults()
}
func (m *Manager) SaveGlobal(p Policy) error {
	p, err := Validate(p)
	if err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err = m.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", globalKey, string(data)); err != nil {
		return err
	}
	changed := m.global.Rate != p.Rate
	m.global = p
	if changed {
		m.counters = map[string]bucket{}
	}
	return nil
}
func (m *Manager) SaveCollection(id string, p Collection) error {
	if id == "" || len(id) > 100 {
		return fmt.Errorf("invalid collection")
	}
	if err := ValidateCollection(p); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err = m.db.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", collectionPrefix+id, string(data)); err != nil {
		return err
	}
	prior, ok := m.collections[id]
	m.collections[id] = p
	if !ok || prior.Rate != p.Rate {
		for key := range m.counters {
			if strings.HasPrefix(key, "collection:"+id+":") {
				delete(m.counters, key)
			}
		}
	}
	return nil
}
func (m *Manager) PageSize(id string, fallback int) (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	def, max := m.global.DefaultPageSize, m.global.MaxPageSize
	if p, ok := m.collections[id]; ok {
		if p.MaxPageSize > 0 && p.MaxPageSize < max {
			max = p.MaxPageSize
		}
		if p.DefaultPageSize > 0 {
			def = p.DefaultPageSize
		}
	}
	if def == 0 {
		def = fallback
	}
	if def > max {
		def = max
	}
	return def, max
}

type Budget struct {
	Limit     int
	Remaining int
	Reset     int64
	Scope     string
}
type Decision struct {
	Allowed    bool
	Busy       bool
	RetryAfter int
	Budgets    []Budget
}

// Check consumes all applicable budgets atomically, or none. Requests rejected
// by one scope cannot exhaust another scope. State is bounded and process local.
func (m *Manager) Check(ip, id string, read bool) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	if m.sweep.IsZero() || now.Sub(m.sweep) >= time.Minute || (len(m.counters) >= m.maxBuckets && now.Sub(m.sweep) >= time.Second) {
		for k, b := range m.counters {
			if !now.Before(b.Until) {
				delete(m.counters, k)
			}
		}
		m.sweep = now
	}
	type candidate struct {
		key    string
		limit  int
		bucket bucket
		scope  string
	}
	list := []candidate{}
	add := func(scope, key string, r Rate) {
		if !r.Enabled {
			return
		}
		kind := "write"
		limit := r.Writes
		if read {
			kind = "read"
			limit = r.Reads
		}
		key += ":" + kind + ":" + ip
		b, ok := m.counters[key]
		if !ok || !now.Before(b.Until) {
			b = bucket{Until: now.Add(time.Duration(r.WindowSeconds) * time.Second)}
		}
		list = append(list, candidate{key: key, limit: limit, bucket: b, scope: scope})
	}
	add("global", "global", m.global.Rate)
	if p, ok := m.collections[id]; ok {
		add("collection", "collection:"+id, p.Rate)
	}
	result := Decision{Allowed: true}
	newKeys := 0
	for _, b := range list {
		if _, ok := m.counters[b.key]; !ok {
			newKeys++
		}
		remaining := max(0, b.limit-b.bucket.Count)
		if remaining == 0 {
			result.Allowed = false
			seconds := int(b.bucket.Until.Sub(now).Seconds() + 0.999999)
			if seconds > result.RetryAfter {
				result.RetryAfter = max(1, seconds)
			}
		}
		result.Budgets = append(result.Budgets, Budget{Limit: b.limit, Remaining: remaining, Reset: b.bucket.Until.Unix(), Scope: b.scope})
	}
	if len(m.counters)+newKeys > m.maxBuckets {
		result.Allowed = false
		result.Busy = true
		result.RetryAfter = 1
		return result
	}
	if result.Allowed {
		for i, b := range list {
			b.bucket.Count++
			m.counters[b.key] = b.bucket
			result.Budgets[i].Remaining--
		}
	}
	return result
}
