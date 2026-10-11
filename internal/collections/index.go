package collections

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/transformations"
)

type indexPayload struct{ Path, Stamp string }
type countedReader struct {
	r io.Reader
	n int64
}

func (r *countedReader) Read(b []byte) (int, error) {
	n, e := r.r.Read(b)
	r.n += int64(n)
	return n, e
}
func (s *Service) indexJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (result string, err error) {
	var payload indexPayload
	if err = json.Unmarshal([]byte(j.Payload), &payload); err != nil {
		return "", err
	}
	c, err := s.Get(j.Resource)
	if err != nil {
		return "", err
	}
	if c.JobID != j.ID || c.Stamp != payload.Stamp {
		return "", fmt.Errorf("index job superseded")
	}
	defer func() {
		if err != nil {
			state := "failed"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			s.DB.Exec(`UPDATE collections SET status=?,error=? WHERE id=? AND job_id=?`, state, err.Error(), c.ID, j.ID)
		}
	}()
	if err = s.safe(payload.Path); err != nil {
		return "", err
	}
	if _, err = s.DB.Exec(`UPDATE collections SET status='indexing' WHERE id=? AND job_id=?`, c.ID, j.ID); err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.IndexDir, s.Reserve); err != nil {
		return "", err
	}
	source, err := os.Open(payload.Path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", err
	}
	if stamp(info) != payload.Stamp {
		return "", fmt.Errorf("source changed before indexing; refresh to retry")
	}
	if s.StreamBytes > 0 && conversion.Streaming(c.Format) && info.Size() > s.StreamBytes {
		return "", fmt.Errorf("source exceeds streaming byte limit")
	}
	policy := s.indexPolicy(c.Format)
	if c.SourceSHA != "" && c.Generation != "" && c.IndexLimits == policy {
		if _, e := os.Stat(filepath.Join(s.IndexDir, c.Generation)); e == nil {
			digest, e := sourceChecksum(ctx, payload.Path)
			if e != nil {
				return "", e
			}
			after, e := os.Stat(payload.Path)
			if e != nil {
				return "", e
			}
			if stamp(after) != payload.Stamp {
				return "", fmt.Errorf("source changed during checksum validation")
			}
			if digest == c.SourceSHA {
				result, e := s.DB.Exec(`UPDATE collections SET status='ready',error='' WHERE id=? AND job_id=? AND source_stamp=? AND EXISTS(SELECT 1 FROM jobs WHERE jobs.id=collections.job_id AND jobs.state IN ('staged','running'))`, c.ID, j.ID, payload.Stamp)
				if e != nil {
					return "", e
				}
				n, _ := result.RowsAffected()
				if n == 0 {
					return "", fmt.Errorf("source validation superseded")
				}
				s.DB.Exec(`UPDATE jobs SET records=?,processed=? WHERE id=?`, c.Rows, info.Size(), j.ID)
				progress(info.Size(), c.Rows)
				return c.ID, nil
			}
		}
	}
	gen := c.ID + "-" + j.ID + ".db"
	final := filepath.Join(s.IndexDir, gen)
	tmp := final + ".tmp"
	defer os.Remove(tmp)
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return "", err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	for _, q := range []string{`PRAGMA journal_mode=OFF`, `PRAGMA synchronous=OFF`, `PRAGMA cache_size=-4096`, `PRAGMA temp_store=FILE`, `CREATE TABLE records (ordinal INTEGER PRIMARY KEY,payload TEXT NOT NULL)`} {
		if _, err = db.Exec(q); err != nil {
			return "", err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()
	stmt, err := tx.Prepare(`INSERT INTO records(ordinal,payload) VALUES(?,?)`)
	if err != nil {
		return "", err
	}
	defer func() {
		if stmt != nil {
			stmt.Close()
		}
	}()
	var count int64
	var batchBytes int64
	schema := map[string]map[string]bool{}
	hash := sha256.New()
	reader := &countedReader{r: io.TeeReader(source, hash)}
	err = conversion.Iterate(ctx, reader, c.Format, s.RecordBytes, s.MaxBytes, func(row any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Keep < > & as written so indexed values match the source text and
		// substring searches see the characters users typed.
		b, err := conversion.MarshalJSON(row)
		if err != nil {
			return err
		}
		if int64(len(b)) > s.RecordBytes {
			return fmt.Errorf("normalized record exceeds %d byte limit", s.RecordBytes)
		}
		if _, err = stmt.Exec(count, string(b)); err != nil {
			return err
		}
		count++
		batchBytes += int64(len(b))
		for _, field := range Schema([]any{row}) {
			if schema[field.Name] == nil {
				if len(schema) >= 2048 {
					return fmt.Errorf("schema exceeds 2048 field limit")
				}
				schema[field.Name] = map[string]bool{}
			}
			for _, typ := range strings.Split(field.Types, ", ") {
				schema[field.Name][typ] = true
			}
		}
		if count%1000 == 0 || batchBytes >= 4<<20 {
			batchBytes = 0
			if err = s.DiskCheck(s.IndexDir, s.Reserve); err != nil {
				return err
			}
			stmt.Close()
			if err = tx.Commit(); err != nil {
				return err
			}
			tx, err = db.Begin()
			if err != nil {
				return err
			}
			stmt, err = tx.Prepare(`INSERT INTO records(ordinal,payload) VALUES(?,?)`)
			if err != nil {
				return err
			}
			progress(reader.n, count)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if err = db.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.IndexDir, s.Reserve); err != nil {
		return "", err
	}
	after, err := os.Stat(payload.Path)
	if err != nil {
		return "", err
	}
	if stamp(after) != payload.Stamp {
		return "", fmt.Errorf("source changed during indexing; refresh to retry")
	}
	if err = s.safe(payload.Path); err != nil {
		return "", err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	verified, e := sourceChecksum(ctx, payload.Path)
	if e != nil {
		return "", e
	}
	if verified != digest {
		return "", fmt.Errorf("source contents changed during indexing; refresh to retry")
	}
	after, err = os.Stat(payload.Path)
	if err != nil {
		return "", err
	}
	if stamp(after) != payload.Stamp {
		return "", fmt.Errorf("source changed during checksum verification")
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return "", err
	}
	if err = os.Rename(tmp, final); err != nil {
		return "", err
	}
	schemaJSON, _ := json.Marshal(sortedFields(schema))
	resultSQL, err := s.DB.Exec(`UPDATE collections SET rows=?,generation=?,schema_json=?,source_sha256=?,index_limits=?,status='ready',error='' WHERE id=? AND job_id=? AND source_stamp=? AND EXISTS(SELECT 1 FROM jobs WHERE jobs.id=collections.job_id AND jobs.state IN ('staged','running'))`, count, gen, string(schemaJSON), digest, policy, c.ID, j.ID, payload.Stamp)
	if err != nil {
		os.Remove(final)
		return "", err
	}
	n, _ := resultSQL.RowsAffected()
	if n == 0 {
		os.Remove(final)
		return "", fmt.Errorf("source index superseded")
	}
	s.DB.Exec(`UPDATE jobs SET records=?,processed=? WHERE id=?`, count, reader.n, j.ID)
	progress(reader.n, count)
	s.prune(c.ID, gen)
	return c.ID, nil
}
func (s *Service) ready(c Collection) (Collection, error) {
	latest, err := s.Get(c.ID)
	if err != nil {
		return latest, err
	}
	if latest.Status != "ready" {
		if latest.Status == "failed" || latest.Status == "cancelled" || latest.Status == "interrupted" {
			return latest, fmt.Errorf("%s: %s", latest.Status, latest.Error)
		}
		return latest, ErrPending
	}
	if err = s.safe(latest.Path); err != nil {
		return latest, err
	}
	info, err := os.Stat(latest.Path)
	if err != nil {
		return latest, err
	}
	if stamp(info) != latest.Stamp {
		if err = s.SyncFile(latest.Path); err != nil {
			return latest, err
		}
		return latest, ErrPending
	}
	return latest, nil
}
func (s *Service) openIndex(c Collection) (*sql.DB, error) {
	if c.Generation == "" || filepath.Base(c.Generation) != c.Generation {
		return nil, ErrPending
	}
	path := filepath.Join(s.IndexDir, c.Generation)
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA cache_size=-4096`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func (s *Service) Page(ctx context.Context, c Collection, page, limit int, apply bool, q string) ([]any, int64, error) {
	c, err := s.ready(c)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	start := int64(page-1) * int64(limit)
	unlock := s.lock(c.ID + "-generation")
	c, err = s.Get(c.ID)
	if err != nil {
		unlock()
		return nil, 0, err
	}
	if c.Status != "ready" {
		unlock()
		return nil, 0, ErrPending
	}
	s.retainGeneration(c.Generation)
	defer s.releaseGeneration(c.ID, c.Generation)
	db, err := s.openIndex(c)
	unlock()
	if err != nil {
		return nil, 0, err
	}
	defer db.Close()
	out := []any{}
	if (!apply || strings.TrimSpace(c.Rules) == "") && q == "" {
		rows, err := db.QueryContext(ctx, `SELECT payload FROM records WHERE ordinal>=? ORDER BY ordinal LIMIT ?`, start, limit)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		for rows.Next() {
			var b string
			if err = rows.Scan(&b); err != nil {
				return nil, 0, err
			}
			v, err := conversion.Decode([]byte(b), "json")
			if err != nil {
				return nil, 0, err
			}
			out = append(out, v)
		}
		return out, c.Rows, rows.Err()
	}
	ruleText := ""
	if apply {
		ruleText = c.Rules
	}
	rules, err := transformations.Parse(ruleText)
	if err != nil {
		return nil, 0, err
	}
	total := int64(0)
	err = s.scanIndex(ctx, db, func(v any) error {
		matched := transformations.Apply([]any{v}, rules)
		if len(matched) == 0 {
			return nil
		}
		v = matched[0]
		if q != "" {
			b, _ := conversion.Encode(v, "json")
			if !strings.Contains(strings.ToLower(string(b)), strings.ToLower(q)) {
				return nil
			}
		}
		if total >= start && total < start+int64(limit) {
			out = append(out, v)
		}
		total++
		return nil
	})
	return out, total, err
}
func (s *Service) scanIndex(ctx context.Context, db *sql.DB, emit func(any) error) error {
	rows, err := db.QueryContext(ctx, `SELECT payload FROM records ORDER BY ordinal`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var b string
		if err = rows.Scan(&b); err != nil {
			return err
		}
		v, err := conversion.Decode([]byte(b), "json")
		if err != nil {
			return err
		}
		if err = emit(v); err != nil {
			return err
		}
	}
	return rows.Err()
}
func (s *Service) Fields(c Collection) ([]Field, error) {
	c, err := s.ready(c)
	if err != nil {
		return nil, err
	}
	var fields []Field
	err = json.Unmarshal([]byte(c.SchemaJSON), &fields)
	return fields, err
}
func (s *Service) Each(ctx context.Context, c Collection, emit func(any) error) error {
	c, err := s.ready(c)
	if err != nil {
		return err
	}
	unlock := s.lock(c.ID + "-generation")
	c, err = s.Get(c.ID)
	if err != nil {
		unlock()
		return err
	}
	if c.Status != "ready" {
		unlock()
		return ErrPending
	}
	s.retainGeneration(c.Generation)
	defer s.releaseGeneration(c.ID, c.Generation)
	db, err := s.openIndex(c)
	unlock()
	if err != nil {
		return err
	}
	defer db.Close()
	return s.scanIndex(ctx, db, emit)
}
func (s *Service) prune(id, current string) {
	unlock := s.lock(id + "-generation")
	defer unlock()
	if c, err := s.Get(id); err == nil {
		current = c.Generation
	} else if errors.Is(err, sql.ErrNoRows) {
		current = ""
	} else {
		return
	}
	files, _ := os.ReadDir(s.IndexDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), id+"-") && strings.HasSuffix(f.Name(), ".db") && f.Name() != current {
			s.mu.Lock()
			busy := s.readers[f.Name()] > 0
			s.mu.Unlock()
			if !busy {
				os.Remove(filepath.Join(s.IndexDir, f.Name()))
			}
		}
	}
}
func (s *Service) retainGeneration(gen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readers == nil {
		s.readers = map[string]int{}
	}
	s.readers[gen]++
}
func (s *Service) releaseGeneration(id, gen string) {
	s.mu.Lock()
	s.readers[gen]--
	last := s.readers[gen] == 0
	if last {
		delete(s.readers, gen)
	}
	s.mu.Unlock()
	if last {
		if c, e := s.Get(id); e != nil || c.Generation != gen {
			s.prune(id, "")
		}
	}
}
func (s *Service) cleanup() {
	rows, err := s.DB.Query(`SELECT generation FROM collections WHERE generation<>''`)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for rows.Next() {
		var gen string
		if rows.Scan(&gen) == nil {
			keep[gen] = true
		}
	}
	rows.Close()
	files, _ := os.ReadDir(s.IndexDir)
	for _, f := range files {
		if !keep[f.Name()] {
			os.Remove(filepath.Join(s.IndexDir, f.Name()))
		}
	}
	s.cleanupDownloads()
}
func pending(err error) bool { return errors.Is(err, ErrPending) }
