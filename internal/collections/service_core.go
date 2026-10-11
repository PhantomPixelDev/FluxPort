package collections

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fluxport/internal/config"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/resources"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var ErrTooLarge = errors.New("file exceeds configured buffered document limit")
var ErrPending = errors.New("collection is being indexed; retry shortly")

type Collection struct {
	FormatNotes string `json:"format_notes,omitempty"`
	ID          string `json:"id"`
	Path        string `json:"-"`
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Format      string `json:"format"`
	Size        int64  `json:"size"`
	Rows        int64  `json:"rows"`
	Updated     string `json:"updated"`
	Enabled     bool   `json:"enabled"`
	CRUD        bool   `json:"crud"`
	ReadOnly    bool   `json:"read_only"`
	Rules       string `json:"rules"`
	Error       string `json:"error,omitempty"`
	Status      string `json:"status"`
	Stamp       string `json:"-"`
	Generation  string `json:"-"`
	JobID       string `json:"job_id,omitempty"`
	SchemaJSON  string `json:"-"`
	SourceSHA   string `json:"-"`
	IndexLimits string `json:"-"`
}
type lockEntry struct {
	mu   sync.Mutex
	refs int
}
type Service struct {
	duckSlots         chan struct{}
	scheduleCancel    context.CancelFunc
	scheduleDone      chan struct{}
	DB                *sql.DB
	Root              string
	IndexDir          string
	DownloadDir       string
	MaxBytes          int64
	RecordBytes       int64
	StreamBytes       int64
	Reserve           uint64
	DuckDBMemoryBytes int64
	DuckDBTempBytes   uint64
	DuckDBTimeout     time.Duration
	DuckDBTempDir     string
	Jobs              *jobs.Manager
	DiskCheck         func(string, uint64) error
	mu                sync.Mutex
	locks             map[string]*lockEntry
	readers           map[string]int
}

func New(db *sql.DB, root string, max int64) (*Service, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	var sequence int
	var dbName, dbPath string
	if err = db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &dbName, &dbPath); err != nil {
		return nil, err
	}
	state := filepath.Dir(dbPath)
	s := &Service{DB: db, Root: resolved, IndexDir: filepath.Join(state, "indexes"), DownloadDir: filepath.Join(state, "downloads"), MaxBytes: max, RecordBytes: 1 << 20, Reserve: 256 << 20, DuckDBMemoryBytes: 512 << 20, DuckDBTempBytes: 4 << 30, DuckDBTimeout: 30 * time.Second, locks: map[string]*lockEntry{}, DiskCheck: resources.Check}
	s.DuckDBTempDir = filepath.Join(state, "duckdb-tmp")
	s.duckSlots = make(chan struct{}, min(2, runtime.GOMAXPROCS(0)))
	for _, dir := range []string{s.IndexDir, s.DownloadDir} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	s.Jobs = jobs.New(db)
	s.Jobs.Register("index", s.indexJob)
	s.Jobs.Register("convert", s.convertJob)
	return s, nil
}
func (s *Service) Configure(c config.Config) {
	if c.Workers > 0 {
		s.duckSlots = make(chan struct{}, min(c.Workers, runtime.GOMAXPROCS(0)))
	}
	s.RecordBytes = c.MaxRecordBytes
	if s.RecordBytes == 0 {
		s.RecordBytes = 1 << 20
	}
	s.Reserve = c.MinFreeDiskBytes
	s.StreamBytes = c.MaxStreamBytes
	if c.DuckDBMemoryBytes > 0 {
		s.DuckDBMemoryBytes = c.DuckDBMemoryBytes
	}
	if c.DuckDBTempBytes > 0 {
		s.DuckDBTempBytes = c.DuckDBTempBytes
	}
	if c.DuckDBTimeoutSeconds > 0 {
		s.DuckDBTimeout = time.Duration(c.DuckDBTimeoutSeconds) * time.Second
	}
	if c.DuckDBTempDir != "" {
		s.DuckDBTempDir = c.DuckDBTempDir
	}
}
func (s *Service) Start(ctx context.Context, workers int) error {
	if err := s.Jobs.Start(ctx, workers); err != nil {
		return err
	}
	_, err := s.DB.Exec(`UPDATE collections SET status='interrupted',error='Indexing interrupted; refresh to retry' WHERE status IN ('pending','indexing')`)
	if err != nil {
		return err
	}
	if _, err = s.DB.Exec(`UPDATE collections SET status='unverified',error='' WHERE status='ready'`); err != nil {
		return err
	}
	s.cleanup()
	s.cleanupDuckDBCaches()
	s.startScheduler(ctx)
	return nil
}
func (s *Service) Stop() {
	if s.scheduleCancel != nil {
		s.scheduleCancel()
		<-s.scheduleDone
	}
	s.Jobs.Stop()
}
func (s *Service) lock(key string) func() {
	s.mu.Lock()
	entry := s.locks[key]
	if entry == nil {
		entry = &lockEntry{}
		s.locks[key] = entry
	}
	entry.refs++
	s.mu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.locks, key)
		}
		s.mu.Unlock()
	}
}

const fields = `id,path,CASE WHEN display_name='' THEN name ELSE display_name END,name,format,size,rows,updated,enabled,crud,rules,error,status,source_stamp,generation,job_id,schema_json,source_sha256,index_limits`

func scan(row interface{ Scan(...any) error }) (Collection, error) {
	var c Collection
	err := row.Scan(&c.ID, &c.Path, &c.Name, &c.Filename, &c.Format, &c.Size, &c.Rows, &c.Updated, &c.Enabled, &c.CRUD, &c.Rules, &c.Error, &c.Status, &c.Stamp, &c.Generation, &c.JobID, &c.SchemaJSON, &c.SourceSHA, &c.IndexLimits)
	if c.Name == c.Filename {
		c.Name = friendly(c.Name)
	}
	return c, err
}
func friendly(name string) string {
	if len(name) > 25 && name[24] == '-' {
		if _, err := hex.DecodeString(name[:24]); err == nil {
			return name[25:]
		}
	}
	return name
}
func (s *Service) Get(id string) (Collection, error) {
	return s.decorate(scan(s.DB.QueryRow(`SELECT `+fields+` FROM collections WHERE id=?`, id)))
}
func (s *Service) byPath(path string) (Collection, error) {
	return s.decorate(scan(s.DB.QueryRow(`SELECT `+fields+` FROM collections WHERE path=?`, path)))
}
func (s *Service) decorate(c Collection, err error) (Collection, error) {
	c.FormatNotes = BinaryNotes(c.Format)
	c.ReadOnly = c.Size > s.MaxBytes || binaryFormat(c.Format)
	if c.ReadOnly {
		c.CRUD = false
	}
	return c, err
}
func (s *Service) List(q string, page, per int) ([]Collection, int, error) {
	if page < 1 {
		page = 1
	}
	if per < 1 || per > 100 {
		per = 20
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM collections WHERE (CASE WHEN display_name='' THEN name ELSE display_name END) LIKE ? ESCAPE '\'`, pattern).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.Query(`SELECT `+fields+` FROM collections WHERE (CASE WHEN display_name='' THEN name ELSE display_name END) LIKE ? ESCAPE '\' ORDER BY display_name,name,id LIMIT ? OFFSET ?`, pattern, per, (page-1)*per)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Collection{}
	for rows.Next() {
		c, err := s.decorate(scan(rows))
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}
func (s *Service) Stats() (count, enabled, rows int, size int64, err error) {
	err = s.DB.QueryRow(`SELECT COUNT(*),COALESCE(SUM(enabled),0),COALESCE(SUM(rows),0),COALESCE(SUM(size),0) FROM collections`).Scan(&count, &enabled, &rows, &size)
	return
}
func (s *Service) IssueCount() int {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM collections WHERE status IN ('failed','interrupted','cancelled')`).Scan(&n)
	return n
}
func (s *Service) Setting(key, fallback string) string {
	var v string
	if err := s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return fallback
	}
	return v
}
func (s *Service) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
func (s *Service) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return fmt.Errorf("display name must contain 1–120 characters")
	}
	result, err := s.DB.Exec(`UPDATE collections SET display_name=? WHERE id=?`, name, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func stamp(info os.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}
func (s *Service) excluded(path string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM exclusions WHERE path=?`, path).Scan(&n)
	return n > 0, err
}
func (s *Service) syncFile(path string, force bool) error {
	format := conversion.FromFilename(path)
	if !datasetSupported(format) {
		return nil
	}
	excluded, err := s.excluded(path)
	if err != nil || excluded {
		return err
	}
	if err = s.safe(path); err != nil {
		if os.IsNotExist(err) {
			c, e := s.byPath(path)
			if e == nil {
				s.Jobs.CancelResource(c.ID)
			}
			_, err = s.DB.Exec(`DELETE FROM collections WHERE path=?`, path)
			return err
		}
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	sourceStamp := stamp(info)
	existing, err := s.byPath(path)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	id := existing.ID
	if id == "" {
		id = jobs.ID()
	}
	indexPresent := false
	if existing.Generation != "" {
		_, e := os.Stat(filepath.Join(s.IndexDir, existing.Generation))
		indexPresent = e == nil
	}
	if !force && existing.Stamp == sourceStamp && ((existing.Status == "ready" && indexPresent) || existing.Status == "pending" || existing.Status == "indexing") {
		return nil
	}
	if err = s.Jobs.CancelResource(id); err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO collections(id,path,name,format,size,rows,updated,status,source_stamp) VALUES(?,?,?,?,?,0,?,'pending',?) ON CONFLICT(path) DO UPDATE SET name=excluded.name,format=excluded.format,size=excluded.size,updated=excluded.updated,status='pending',source_stamp=excluded.source_stamp,error=''`, id, path, filepath.Base(path), format, info.Size(), info.ModTime().UTC().Format(time.RFC3339), sourceStamp)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(indexPayload{Path: path, Stamp: sourceStamp})
	job, err := s.Jobs.Submit("index", id, friendly(filepath.Base(path)), string(payload), info.Size())
	if err != nil {
		return err
	}
	if _, err = s.DB.Exec(`UPDATE collections SET job_id=? WHERE id=?`, job.ID, id); err != nil {
		return err
	}
	if s.Jobs.Started() {
		return s.Jobs.Activate(job.ID)
	}
	if !s.Jobs.Started() {
		_, err = s.indexJob(context.Background(), job, func(int64, int64) {})
		state, msg := "ready", ""
		if err != nil {
			state = "failed"
			msg = err.Error()
		}
		s.DB.Exec(`UPDATE jobs SET state=?,error=?,processed=total WHERE id=?`, state, msg, job.ID)
		return nil
	}
	return nil
}
func (s *Service) SyncFile(path string) error {
	unlock := s.lock(path)
	defer unlock()
	return s.syncFile(path, true)
}
func (s *Service) SyncChangedFile(path string) error {
	unlock := s.lock(path)
	defer unlock()
	return s.syncFile(path, false)
}

// ReconcileSubtree handles directory renames/removals without walking unrelated files.
func (s *Service) ReconcileSubtree(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			return s.SyncChangedFile(p)
		})
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	rows, err := s.DB.Query(`SELECT path FROM collections`)
	if err != nil {
		return err
	}
	paths := []string{}
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		if strings.HasPrefix(p, path+string(filepath.Separator)) {
			paths = append(paths, p)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err = s.SyncChangedFile(p); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Sync() error {
	seen := map[string]bool{}
	err := filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != s.Root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || strings.HasPrefix(d.Name(), ".") || !datasetSupported(conversion.FromFilename(path)) {
			return nil
		}
		seen[path] = true
		unlock := s.lock(path)
		defer unlock()
		return s.syncFile(path, true)
	})
	if err != nil {
		return err
	}
	rows, err := s.DB.Query(`SELECT id,path FROM collections`)
	if err != nil {
		return err
	}
	stale := []Collection{}
	for rows.Next() {
		var c Collection
		if err = rows.Scan(&c.ID, &c.Path); err != nil {
			rows.Close()
			return err
		}
		if !seen[c.Path] {
			stale = append(stale, c)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range stale {
		if err = s.Jobs.CancelResource(c.ID); err != nil {
			return err
		}
		if _, err = s.DB.Exec(`DELETE FROM collections WHERE id=?`, c.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Read(c Collection) (any, error) {
	unlock := s.lock(c.Path)
	defer unlock()
	b, err := s.read(c.Path)
	if err != nil {
		return nil, err
	}
	return conversion.Decode(b, c.Format)
}
func (s *Service) Import(name string, r io.Reader) (Collection, error) {
	path, err := s.Stage(name, r)
	if err != nil {
		return Collection{}, err
	}
	if !conversion.Streaming(conversion.FromFilename(name)) && !binaryFormat(conversion.FromFilename(name)) {
		b, err := s.read(path)
		if err != nil {
			os.Remove(path)
			return Collection{}, err
		}
		if _, err = conversion.Decode(b, conversion.FromFilename(name)); err != nil {
			os.Remove(path)
			return Collection{}, err
		}
	}
	if err = s.SyncFile(path); err != nil {
		os.Remove(path)
		return Collection{}, err
	}
	c, err := s.byPath(path)
	if err == nil && c.Status == "failed" {
		os.Remove(path)
		s.DB.Exec(`DELETE FROM collections WHERE id=?`, c.ID)
		return Collection{}, errors.New(c.Error)
	}
	return c, err
}
func (s *Service) Stage(name string, r io.Reader) (string, error) {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	format := conversion.FromFilename(name)
	if !datasetSupported(format) {
		return "", fmt.Errorf("supported formats: json, csv, yaml, xml, ndjson, toml, xlsx, parquet")
	}
	if len(name) > 180 {
		return "", fmt.Errorf("filename is too long")
	}
	if err := s.DiskCheck(s.Root, s.Reserve); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.Root, ".fluxport-upload-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	limit := s.StreamBytes
	if !conversion.Streaming(format) && format != "parquet" {
		limit = s.MaxBytes
	}
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		n, e := r.Read(buffer)
		if n > 0 {
			total += int64(n)
			if limit > 0 && total > limit {
				f.Close()
				return "", ErrTooLarge
			}
			if total%(4<<20) < int64(n) {
				if err = s.DiskCheck(s.Root, s.Reserve); err != nil {
					f.Close()
					return "", err
				}
			}
			if _, err = f.Write(buffer[:n]); err != nil {
				f.Close()
				return "", err
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			f.Close()
			return "", e
		}
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	path := filepath.Join(s.Root, jobs.ID()+"-"+name)
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
func (s *Service) Remove(id string) error {
	c, err := s.Get(id)
	if err != nil {
		return err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	if err = s.Jobs.CancelResource(id); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO exclusions(path,name,removed) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET name=excluded.name,removed=excluded.removed`, c.Path, c.Name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM collections WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type Excluded struct{ Path, Name, Removed, Key string }

func (s *Service) Excluded() ([]Excluded, error) {
	rows, err := s.DB.Query(`SELECT path,name,removed FROM exclusions ORDER BY removed DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Excluded{}
	for rows.Next() {
		var e Excluded
		if err = rows.Scan(&e.Path, &e.Name, &e.Removed); err != nil {
			return nil, err
		}
		e.Key = hex.EncodeToString([]byte(e.Path))
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Service) Restore(key string) error {
	b, err := hex.DecodeString(key)
	if err != nil {
		return fmt.Errorf("invalid exclusion")
	}
	path := string(b)
	var name string
	if err = s.DB.QueryRow(`SELECT name FROM exclusions WHERE path=?`, path).Scan(&name); err != nil {
		return err
	}
	if err = s.safe(path); err != nil {
		return err
	}
	if _, err = s.DB.Exec(`DELETE FROM exclusions WHERE path=?`, path); err != nil {
		return err
	}
	if err = s.SyncFile(path); err != nil {
		return err
	}
	c, err := s.byPath(path)
	if err != nil {
		return err
	}
	if c.Name == name {
		return nil
	}
	return s.Rename(c.ID, name)
}
func (s *Service) Delete(id string) error {
	c, err := s.Get(id)
	if err != nil {
		return err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	if err = s.Jobs.CancelResource(id); err != nil {
		return err
	}
	if err = s.safe(c.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.Remove(c.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err = s.DB.Exec(`DELETE FROM collections WHERE id=?`, id)
	return err
}
