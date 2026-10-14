package collections

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
)

type preparePayload struct {
	Key         string
	Pipeline    Pipeline
	Generations map[string]string
}

// PreparedResult describes a disposable immutable result, never a collection.
type PreparedResult struct {
	ID           string            `json:"id"`
	CollectionID string            `json:"collection_id"`
	Key          string            `json:"key"`
	Rows         int64             `json:"rows"`
	Bytes        int64             `json:"bytes"`
	Fields       []Field           `json:"fields"`
	Generations  map[string]string `json:"generations"`
	InputQuery   *Query            `json:"input_query,omitempty"`
	Created      time.Time         `json:"created"`
	Expires      time.Time         `json:"expires"`
}

func (s *Service) PrepareResult(p Pipeline) (jobs.Job, error) {
	if p.InputQuery != nil {
		q := NormalizeInputQuery(*p.InputQuery)
		p.InputQuery = &q
	}
	_, ids, err := CompilePipeline(p)
	if err != nil {
		return jobs.Job{}, err
	}
	generations := map[string]string{}
	var source Collection
	for _, id := range ids {
		c, e := s.Get(id)
		if e != nil {
			return jobs.Job{}, e
		}
		c, e = s.ready(c)
		if e != nil {
			return jobs.Job{}, e
		}
		generations[id] = c.Generation
		if id == p.Source {
			source = c
		}
	}
	// Names and save revisions do not change the data. Canonical JSON sorts maps.
	p.ID, p.Name, p.Updated, p.Revision = "", "", "", 0
	p.Version = 1
	encoded, err := json.Marshal(preparePayload{Pipeline: p, Generations: generations})
	if err != nil {
		return jobs.Job{}, err
	}
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	unlock := s.lock("prepare-" + key)
	defer unlock()
	var existing string
	err = s.DB.QueryRow(`SELECT id FROM jobs WHERE kind='prepare_result' AND resource=? AND json_extract(payload,'$.Key')=? AND state IN ('queued','running','ready') ORDER BY created DESC,id DESC LIMIT 1`, p.Source, key).Scan(&existing)
	if err == nil {
		j, e := s.Jobs.Get(existing)
		if e == nil && j.State != "ready" {
			return j, nil
		}
		if e == nil {
			if _, e = s.PreparedResult(p.Source, existing); e == nil {
				return j, nil
			}
		}
	} else if err != sql.ErrNoRows {
		return jobs.Job{}, err
	}
	s.cleanupResults()
	encoded, err = json.Marshal(preparePayload{Key: key, Pipeline: p, Generations: generations})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("prepare_result", p.Source, source.Name+" · prepare cleaned data", string(encoded), source.Size)
}

func (s *Service) prepareResultJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (string, error) {
	var p preparePayload
	decoder := json.NewDecoder(strings.NewReader(j.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&p); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := s.DiskCheck(s.ResultDir, s.Reserve); err != nil {
		return "", err
	}
	path := filepath.Join(s.ResultDir, j.ID+".db")
	tmp := path + ".tmp"
	manifest := filepath.Join(s.ResultDir, j.ID+".json")
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return "", err
	}
	db.SetMaxOpenConns(1)
	published := false
	defer func() {
		db.Close()
		os.Remove(tmp)
		os.Remove(manifest + ".tmp")
		if !published {
			os.Remove(path)
			os.Remove(manifest)
		}
	}()
	if _, err = db.ExecContext(ctx, `PRAGMA journal_mode=OFF; PRAGMA synchronous=OFF; PRAGMA cache_size=-4096; PRAGMA temp_store=FILE; CREATE TABLE records(ordinal INTEGER PRIMARY KEY,record_id TEXT UNIQUE NOT NULL,payload TEXT NOT NULL)`); err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()
	var rows, bytes int64
	batchBytes := int64(0)
	schema := map[string]map[string]bool{}
	err = s.eachPipeline(ctx, p.Pipeline, p.Generations, func(value any) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		b, e := conversion.MarshalJSON(value)
		if e != nil {
			return e
		}
		if int64(len(b)) > s.RecordBytes {
			return fmt.Errorf("cleaned record exceeds configured record limit")
		}
		for _, field := range Schema([]any{value}) {
			if schema[field.Name] == nil {
				if len(schema) >= 2048 {
					return fmt.Errorf("cleaned schema exceeds 2048 fields")
				}
				schema[field.Name] = map[string]bool{}
			}
			for _, t := range strings.Split(field.Types, ", ") {
				schema[field.Name][t] = true
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO records VALUES(?,?,?)`, rows, fmt.Sprintf("%s-%d", j.ID, rows), string(b)); e != nil {
			return e
		}
		rows++
		bytes += int64(len(b))
		batchBytes += int64(len(b))
		if rows%1000 == 0 || batchBytes >= 4<<20 {
			if e = s.DiskCheck(s.ResultDir, s.Reserve); e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			tx, e = db.BeginTx(ctx, nil)
			if e != nil {
				return e
			}
			batchBytes = 0
			progress(min(bytes, j.Total), rows)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if err = db.Close(); err != nil {
		return "", err
	}
	// Verify dependencies again after the scan. A changed source never publishes.
	if err = s.checkResultGenerations(p.Generations); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.ResultDir, s.Reserve); err != nil {
		return "", err
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
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	d := PreparedResult{ID: j.ID, CollectionID: j.Resource, Key: p.Key, Rows: rows, Bytes: bytes, Fields: sortedFields(schema), Generations: p.Generations, InputQuery: p.Pipeline.InputQuery, Created: now, Expires: now.Add(24 * time.Hour)}
	if rows == 0 {
		d.Fields = s.emptyPipelineFields(p.Pipeline)
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	f, err = os.OpenFile(manifest+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(manifest+".tmp", manifest); err != nil {
		return "", err
	}
	if _, err = s.DB.Exec(`UPDATE jobs SET records=? WHERE id=?`, rows, j.ID); err != nil {
		return "", err
	}
	progress(j.Total, rows)
	published = true
	return manifest, nil
}

func (s *Service) checkResultGenerations(generations map[string]string) error {
	for id, gen := range generations {
		c, err := s.Get(id)
		if err != nil {
			return err
		}
		c, err = s.ready(c)
		if err != nil {
			return err
		}
		if c.Generation != gen {
			return fmt.Errorf("source changed; prepare cleaned data again: %w", ErrConflict)
		}
	}
	return nil
}

func (s *Service) PreparedResult(owner, id string) (PreparedResult, error) {
	d, ref, err := s.resultReference(owner, id)
	if err == nil {
		ref.release()
	}
	return d, err
}

func (s *Service) PreparedDefinition(owner, id string) (Pipeline, string, error) {
	j, err := s.Jobs.Get(id)
	if err != nil {
		return Pipeline{}, "", err
	}
	if j.Kind != "prepare_result" || j.Resource != owner {
		return Pipeline{}, "", fmt.Errorf("cleaned result does not belong to this collection")
	}
	var p preparePayload
	d := json.NewDecoder(strings.NewReader(j.Payload))
	d.UseNumber()
	if err = d.Decode(&p); err != nil {
		return Pipeline{}, "", err
	}
	return p.Pipeline, p.Generations[owner], nil
}

func (s *Service) resultReference(owner, id string) (PreparedResult, indexReference, error) {
	var d PreparedResult
	var ref indexReference
	j, err := s.Jobs.Get(id)
	if err != nil {
		return d, ref, err
	}
	if j.Kind != "prepare_result" || j.Resource != owner {
		return d, ref, fmt.Errorf("cleaned result does not belong to this collection")
	}
	if j.State != "ready" {
		if j.State == "queued" || j.State == "running" {
			return d, ref, ErrPending
		}
		return d, ref, fmt.Errorf("cleaned data %s: %s; retry preparation", j.State, j.Error)
	}
	if j.Result != filepath.Join(s.ResultDir, j.ID+".json") {
		return d, ref, fmt.Errorf("invalid cleaned result")
	}
	unlock := s.lock("result-" + id)
	defer unlock()
	b, err := os.ReadFile(j.Result)
	if err != nil {
		return d, ref, fmt.Errorf("cleaned data expired; prepare it again")
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return d, ref, err
	}
	if d.ID != id || d.CollectionID != owner || time.Now().After(d.Expires) {
		return d, ref, fmt.Errorf("cleaned data expired; prepare it again")
	}
	if err = s.checkResultGenerations(d.Generations); err != nil {
		return d, ref, err
	}
	path := filepath.Join(s.ResultDir, id+".db")
	if _, err = os.Stat(path); err != nil {
		return d, ref, err
	}
	key := "result:" + id
	s.retainGeneration(key)
	ref = indexReference{path: path, generation: id, rows: d.Rows, fields: d.Fields, release: func() {
		s.mu.Lock()
		s.readers[key]--
		if s.readers[key] == 0 {
			delete(s.readers, key)
		}
		s.mu.Unlock()
	}}
	return d, ref, nil
}

func (s *Service) QueryResult(ctx context.Context, owner, id string, q Query) (QueryResult, error) {
	_, ref, err := s.resultReference(owner, id)
	if err != nil {
		return QueryResult{}, err
	}
	defer ref.release()
	return s.queryReference(ctx, ref, q)
}

func (s *Service) cleanupResults() {
	files, _ := os.ReadDir(s.ResultDir)
	for _, file := range files {
		name := file.Name()
		id := strings.SplitN(name, ".", 2)[0]
		unlock := s.lock("result-" + id)
		s.mu.Lock()
		busy := s.readers["result:"+id] > 0
		s.mu.Unlock()
		if !busy {
			j, e := s.Jobs.Get(id)
			active := e == nil && (j.State == "queued" || j.State == "running")
			info, statErr := file.Info()
			if statErr == nil && !active && (strings.HasSuffix(name, ".tmp") || e != nil || j.State != "ready" || time.Since(info.ModTime()) > 24*time.Hour) {
				os.Remove(filepath.Join(s.ResultDir, name))
			}
		}
		unlock()
	}
}
