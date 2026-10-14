package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Job struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Resource  string `json:"resource"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Payload   string `json:"-"`
	Processed int64  `json:"processed"`
	Total     int64  `json:"total"`
	Records   int64  `json:"records"`
	Error     string `json:"error,omitempty"`
	Result    string `json:"-"`
	Created   string `json:"created"`
	Updated   string `json:"updated"`
}
type Handler func(context.Context, Job, func(int64, int64)) (string, error)
type Manager struct {
	db          *sql.DB
	ctx         context.Context
	cancel      context.CancelFunc
	wake        chan struct{}
	mu          sync.Mutex
	active      map[string]context.CancelFunc
	handlers    map[string]Handler
	subscribers map[chan string]bool
	wg          sync.WaitGroup
	started     bool
}

const columns = `id,kind,resource,name,state,payload,processed,total,records,error,result,created,updated`

func New(db *sql.DB) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{db: db, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), active: map[string]context.CancelFunc{}, handlers: map[string]Handler{}, subscribers: map[chan string]bool{}}
}
func ID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func scan(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := row.Scan(&j.ID, &j.Kind, &j.Resource, &j.Name, &j.State, &j.Payload, &j.Processed, &j.Total, &j.Records, &j.Error, &j.Result, &j.Created, &j.Updated)
	return j, err
}
func (m *Manager) Register(kind string, h Handler) { m.handlers[kind] = h }
func (m *Manager) Start(parent context.Context, workers int) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	m.mu.Unlock()
	if workers < 1 {
		workers = 1
	}
	if _, err := m.db.Exec(`UPDATE jobs SET state='interrupted',error='Application stopped before completion',updated=? WHERE state IN ('queued','running','staged')`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		select {
		case <-parent.Done():
			m.cancel()
		case <-m.ctx.Done():
		}
	}()
	for i := 0; i < workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return nil
}
func (m *Manager) Stop()         { m.cancel(); m.wg.Wait() }
func (m *Manager) Started() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.started }
func (m *Manager) Submit(kind, resource, name, payload string, total int64) (Job, error) {
	state := "queued"
	if kind == "index" {
		state = "staged"
	}
	return m.submit(kind, resource, name, payload, total, state)
}

// SubmitWaiting persists a dependency without consuming a processing worker.
// payload.depends_on identifies the prerequisite job.
func (m *Manager) SubmitWaiting(kind, resource, name, payload string, total int64) (Job, error) {
	return m.submit(kind, resource, name, payload, total, "staged")
}

func (m *Manager) submit(kind, resource, name, payload string, total int64, state string) (Job, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	j := Job{ID: ID(), Kind: kind, Resource: resource, Name: name, State: state, Payload: payload, Total: total, Created: now, Updated: now}
	_, err := m.db.Exec(`INSERT INTO jobs(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.Kind, j.Resource, j.Name, j.State, j.Payload, 0, total, 0, "", "", now, now)
	if err != nil {
		return j, err
	}
	m.notify(j.ID)
	m.signal()
	return j, nil
}
func (m *Manager) Get(id string) (Job, error) {
	return scan(m.db.QueryRow(`SELECT `+columns+` FROM jobs WHERE id=?`, id))
}
func (m *Manager) List() ([]Job, error) {
	return m.ListResource("")
}
func (m *Manager) ListResource(resource string) ([]Job, error) {
	rows, err := m.db.Query(`SELECT `+columns+` FROM jobs WHERE (?='' OR resource=?) ORDER BY created DESC,id DESC LIMIT 100`, resource, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	m.mu.Unlock()
	result, err := m.db.Exec(`UPDATE jobs SET state='cancelled',error='Cancelled by user',updated=? WHERE id=? AND state IN ('queued','running','staged')`, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		if _, err = m.Get(id); err != nil {
			return err
		}
	}
	if n > 0 {
		_, err = m.db.Exec(`UPDATE collections SET status='cancelled',error='Cancelled by user' WHERE job_id=? AND status IN ('pending','indexing')`, id)
		if err != nil {
			return err
		}
	}
	m.notify(id)
	m.signal()
	return nil
}
func (m *Manager) CancelResource(resource string) error {
	rows, err := m.db.Query(`SELECT id FROM jobs WHERE resource=? AND state IN ('queued','running','staged')`, resource)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = m.Cancel(id); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) Retry(id string) (Job, error) {
	j, err := m.Get(id)
	if err != nil {
		return j, err
	}
	if j.State != "failed" && j.State != "cancelled" && j.State != "interrupted" {
		return j, fmt.Errorf("only failed, cancelled, or interrupted jobs can be retried")
	}
	return m.Submit(j.Kind, j.Resource, j.Name, j.Payload, j.Total)
}
func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) notify(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subscribers {
		select {
		case ch <- id:
		default:
			// A coalesced invalidation prevents a dropped final update leaving a
			// preview pending forever during a large batch.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- "*":
			default:
			}
		}
	}
}
func (m *Manager) Subscribe() (chan string, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.subscribers) >= 32 {
		return nil, nil, fmt.Errorf("too many progress subscribers")
	}
	ch := make(chan string, 8)
	m.subscribers[ch] = true
	return ch, func() { m.mu.Lock(); delete(m.subscribers, ch); m.mu.Unlock() }, nil
}
func (m *Manager) claim() (Job, error) {
	if err := m.resolveDependencies(); err != nil {
		return Job{}, err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()
	j, err := scan(tx.QueryRow(`SELECT ` + columns + ` FROM jobs WHERE state='queued' ORDER BY created,id LIMIT 1`))
	if err != nil {
		return j, err
	}
	_, err = tx.Exec(`UPDATE jobs SET state='running',updated=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), j.ID)
	if err != nil {
		return j, err
	}
	err = tx.Commit()
	j.State = "running"
	return j, err
}
func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		if m.ctx.Err() != nil {
			return
		}
		j, err := m.claim()
		if errors.Is(err, sql.ErrNoRows) {
			select {
			case <-m.ctx.Done():
				return
			case <-m.wake:
			}
			continue
		}
		if err != nil {
			slog.Error("claim job", "error", err)
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		m.signal()
		m.execute(j)
	}
}
func (m *Manager) execute(j Job) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.mu.Lock()
	m.active[j.ID] = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); delete(m.active, j.ID); m.mu.Unlock() }()
	m.notify(j.ID)
	current, err := m.Get(j.ID)
	if err != nil || current.State != "running" {
		return
	}
	last := time.Time{}
	progress := func(bytes, records int64) {
		if time.Since(last) < 150*time.Millisecond {
			return
		}
		last = time.Now()
		if _, err := m.db.Exec(`UPDATE jobs SET processed=?,records=?,updated=? WHERE id=? AND state='running'`, bytes, records, time.Now().UTC().Format(time.RFC3339), j.ID); err != nil {
			slog.Warn("job progress", "error", err)
		}
		m.notify(j.ID)
	}
	h := m.handlers[j.Kind]
	result := ""
	if h == nil {
		err = fmt.Errorf("unsupported job kind %q", j.Kind)
	} else {
		result, err = invoke(h, ctx, j, progress)
	}
	state, errorText := "ready", ""
	if err != nil {
		state = "failed"
		errorText = err.Error()
	}
	if ctx.Err() != nil {
		state = "cancelled"
		if m.ctx.Err() != nil {
			state = "interrupted"
		}
		errorText = ctx.Err().Error()
	}
	if _, e := m.db.Exec(`UPDATE jobs SET state=?,error=?,result=?,processed=CASE WHEN ?='ready' THEN total ELSE processed END,updated=? WHERE id=? AND state='running'`, state, errorText, result, state, time.Now().UTC().Format(time.RFC3339), j.ID); e != nil {
		slog.Error("finish job", "error", e)
	}
	m.notify(j.ID)
	m.signal()
}

func (m *Manager) resolveDependencies() error {
	rows, err := m.db.Query(`UPDATE jobs SET state=CASE WHEN (SELECT state FROM jobs d WHERE d.id=json_extract(jobs.payload,'$.depends_on'))='ready' THEN 'queued' ELSE 'failed' END,error=CASE WHEN (SELECT state FROM jobs d WHERE d.id=json_extract(jobs.payload,'$.depends_on'))='ready' THEN '' ELSE 'Cleaned data could not be prepared. Retry preparation, then download again.' END,updated=? WHERE state='staged' AND kind='convert' AND json_valid(payload) AND json_extract(payload,'$.depends_on') IS NOT NULL AND COALESCE((SELECT state FROM jobs d WHERE d.id=json_extract(jobs.payload,'$.depends_on')),'missing') IN ('ready','failed','cancelled','interrupted','missing') RETURNING id`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	for _, id := range ids {
		m.notify(id)
	}
	return err
}

func (m *Manager) Activate(id string) error {
	_, err := m.db.Exec(`UPDATE jobs SET state='queued' WHERE id=? AND state='staged'`, id)
	m.signal()
	return err
}

func invoke(h Handler, ctx context.Context, j Job, p func(int64, int64)) (result string, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("processing failed: %v", v)
		}
	}()
	return h(ctx, j, p)
}

func (m *Manager) Failed(name string, cause error) (Job, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	j := Job{ID: ID(), Kind: "import", Name: name, State: "failed", Error: cause.Error(), Created: now, Updated: now}
	_, err := m.db.Exec(`INSERT INTO jobs(`+columns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.Kind, "", j.Name, j.State, "", 0, 0, 0, j.Error, "", now, now)
	m.notify(j.ID)
	return j, err
}
