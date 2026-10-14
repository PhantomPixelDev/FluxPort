package collections

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
)

// indexReference pins one immutable dataset for the entire read operation.
// Only the service constructs references; clients cannot supply a filesystem path.
type indexReference struct {
	path, generation string
	rows             int64
	fields           []Field
	release          func()
}

func (s *Service) sourceReference(c Collection, expected string) (indexReference, error) {
	var ref indexReference
	c, err := s.ready(c)
	if err != nil {
		return ref, err
	}
	unlock := s.lock(c.ID + "-generation")
	defer unlock()
	c, err = s.Get(c.ID)
	if err != nil {
		return ref, err
	}
	if c.Status != "ready" || c.Generation == "" || filepath.Base(c.Generation) != c.Generation {
		return ref, ErrPending
	}
	if expected != "" && expected != c.Generation {
		return ref, ErrConflict
	}
	if err = json.Unmarshal([]byte(c.SchemaJSON), &ref.fields); err != nil {
		return ref, err
	}
	ref.path = filepath.Join(s.IndexDir, c.Generation)
	ref.generation, ref.rows = c.Generation, c.Rows
	s.retainGeneration(c.Generation)
	ref.release = func() { s.releaseGeneration(c.ID, c.Generation) }
	return ref, nil
}

func openReference(ref indexReference) (*sql.DB, error) {
	if _, err := os.Stat(ref.path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", ref.path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA cache_size=-4096; PRAGMA temp_store=FILE; PRAGMA query_only=ON`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
