package apipolicy

import (
	"fluxport/internal/database"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func manager(t *testing.T) *Manager {
	t.Helper()
	db, err := database.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestAtomicBudgetsAndExpiry(t *testing.T) {
	m := manager(t)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }
	p := Defaults()
	p.Rate.Enabled = true
	p.Rate.Reads = 10
	if err := m.SaveGlobal(p); err != nil {
		t.Fatal(err)
	}
	c := CollectionDefaults()
	c.Rate.Enabled = true
	c.Rate.Reads = 2
	if err := m.SaveCollection("a", c); err != nil {
		t.Fatal(err)
	}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.Check("ip", "a", true).Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 2 {
		t.Fatal(allowed.Load())
	}
	d := m.Check("ip", "a", true)
	if d.Allowed || d.RetryAfter != 60 || d.Budgets[0].Remaining != 8 {
		t.Fatal(d)
	}
	for i := 0; i < 8; i++ {
		if !m.Check("ip", "b", true).Allowed {
			t.Fatal("denied collection consumed global budget")
		}
	}
	if m.Check("ip", "b", true).Allowed {
		t.Fatal("global budget bypassed")
	}
	if !m.Check("ip", "a", false).Allowed || !m.Check("other", "a", true).Allowed {
		t.Fatal("independent budgets")
	}
	now = now.Add(time.Minute)
	if !m.Check("ip", "a", true).Allowed {
		t.Fatal("window did not expire")
	}
}
func TestBoundedCounters(t *testing.T) {
	m := manager(t)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }
	m.maxBuckets = 2
	p := Defaults()
	p.Rate.Enabled = true
	if err := m.SaveGlobal(p); err != nil {
		t.Fatal(err)
	}
	m.Check("one", "", true)
	m.Check("two", "", true)
	if d := m.Check("three", "", true); !d.Busy || d.Allowed || len(m.counters) != 2 {
		t.Fatal(d)
	}
	now = now.Add(time.Minute)
	if d := m.Check("three", "", true); !d.Allowed || len(m.counters) != 1 {
		t.Fatal(d)
	}
}
func TestPersistencePaginationAndValidation(t *testing.T) {
	m := manager(t)
	p := Defaults()
	p.DefaultPageSize = 8
	p.MaxPageSize = 15
	p.Origins = []string{"https://EXAMPLE.com", "https://example.com"}
	if err := m.SaveGlobal(p); err != nil {
		t.Fatal(err)
	}
	c := CollectionDefaults()
	c.MaxPageSize = 5
	c.DefaultPageSize = 4
	if err := m.SaveCollection("abc", c); err != nil {
		t.Fatal(err)
	}
	n, err := New(m.db)
	if err != nil {
		t.Fatal(err)
	}
	if def, max := n.PageSize("abc", 50); def != 4 || max != 5 {
		t.Fatal(def, max)
	}
	if def, max := n.PageSize("", 50); def != 8 || max != 15 {
		t.Fatal(def, max)
	}
	if len(n.Global().Origins) != 1 || n.Global().Origins[0] != "https://example.com" {
		t.Fatal(n.Global())
	}
	for _, origins := range [][]string{{"*", "https://a.com"}, {"https://a.com/path"}, {"file:///tmp"}, {"https://user:pass@a.com"}, {"https://a.com\r\nx:y"}} {
		p.Origins = origins
		if err := n.SaveGlobal(p); err == nil {
			t.Fatal(origins)
		}
	}
	if n.Global().MaxPageSize != 15 {
		t.Fatal("invalid update changed policy")
	}
	copy := n.Global()
	copy.Origins[0] = "bad"
	if n.Global().Origins[0] == "bad" {
		t.Fatal("mutable policy")
	}
}
func TestProxyTrust(t *testing.T) {
	m := manager(t)
	var err error
	m.proxies, err = ParseProxies([]string{"127.0.0.1", "10.0.0.0/8", "::1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"192.0.2.1", "1.2.3.4", "192.0.2.1"},
		{"127.0.0.1", "1.2.3.4, 10.1.2.3", "1.2.3.4"},
		{"127.0.0.1", "8.8.8.8, 192.0.2.9", "192.0.2.9"},
		{"127.0.0.1", "invalid, 10.1.2.3", "127.0.0.1"},
		{"::1", "2001:db8::9", "2001:db8::9"},
	} {
		if got := m.ClientIP(tc.peer, tc.forwarded); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	if _, err := ParseProxies([]string{"not an ip"}); err == nil {
		t.Fatal("invalid proxy accepted")
	}
}
