package collections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"fluxport/internal/conversion"
)

var ErrConflict = errors.New("source changed; reload the collection before editing")

type Mutation struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	Value   any    `json:"value,omitempty"`
	Ordinal int    `json:"-"`
}

func (s *Service) BulkMutate(id, generation string, changes []Mutation) ([]any, error) {
	if generation == "" {
		return nil, fmt.Errorf("generation is required")
	}
	return s.mutateRecords(id, generation, changes, true)
}

func newRecordID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// Validate the complete batch before a single atomic source replacement.
func (s *Service) mutateRecords(id, generation string, changes []Mutation, stable bool) ([]any, error) {
	if len(changes) == 0 || len(changes) > 1000 {
		return nil, fmt.Errorf("select between 1 and 1000 records")
	}
	c, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	c, err = s.ready(c)
	if err != nil {
		return nil, err
	}
	if generation != "" && generation != c.Generation {
		return nil, ErrConflict
	}
	if c.ReadOnly || c.Size > s.MaxBytes {
		return nil, fmt.Errorf("large collections are read-only")
	}
	if !c.Enabled || !c.CRUD {
		return nil, fmt.Errorf("CRUD is disabled")
	}
	if c.Rules != "" && c.Rules != "{}" {
		return nil, fmt.Errorf("remove transformations before enabling writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	b, err := s.read(c.Path)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != c.SourceSHA {
		return nil, ErrConflict
	}
	v, err := conversion.Decode(b, c.Format)
	if err != nil {
		return nil, err
	}
	values, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("CRUD requires an array at the source root")
	}
	if len(values) > 100000 {
		return nil, fmt.Errorf("CRUD supports at most 100000 source records")
	}
	s.retainGeneration(c.Generation)
	defer s.releaseGeneration(c.ID, c.Generation)
	db, err := s.openIndex(c)
	if err != nil {
		return nil, err
	}
	r, err := db.QueryContext(ctx, `SELECT record_id FROM records ORDER BY ordinal`)
	if err != nil {
		db.Close()
		return nil, err
	}
	ids := make([]string, 0, len(values))
	positions := map[string]int{}
	for r.Next() {
		var rid string
		if err = r.Scan(&rid); err != nil {
			break
		}
		positions[rid] = len(ids)
		ids = append(ids, rid)
	}
	if err == nil {
		err = r.Err()
	}
	r.Close()
	db.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) != len(values) {
		return nil, ErrConflict
	}
	deleted := map[int]bool{}
	seen := map[int]bool{}
	results := []any{}
	for _, change := range changes {
		i := change.Ordinal
		if stable && change.Method != "POST" {
			var exists bool
			i, exists = positions[change.ID]
			if !exists {
				return nil, sql.ErrNoRows
			}
		}
		if change.Method != "POST" {
			if i < 0 || i >= len(values) {
				return nil, sql.ErrNoRows
			}
			if seen[i] {
				return nil, fmt.Errorf("record selected more than once")
			}
			seen[i] = true
		}
		switch change.Method {
		case "POST":
			values = append(values, change.Value)
			ids = append(ids, newRecordID())
			results = append(results, change.Value)
		case "PUT":
			values[i] = change.Value
			results = append(results, change.Value)
		case "PATCH":
			object, ok := values[i].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("PATCH requires an object row")
			}
			patch, ok := change.Value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("PATCH body must be an object")
			}
			for k, value := range patch {
				if value == nil {
					delete(object, k)
				} else {
					object[k] = value
				}
			}
			results = append(results, object)
		case "FIELD":
			edit, ok := change.Value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("field edit must contain field and value")
			}
			field, ok := edit["field"].(string)
			if !ok {
				return nil, fmt.Errorf("field path is required")
			}
			if _, err = jsonPath(field); err != nil {
				return nil, err
			}
			value, exists := edit["value"]
			if !exists {
				return nil, fmt.Errorf("field value is required")
			}
			object, ok := values[i].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("field edits require an object record")
			}
			parts, e := fieldParts(field)
			if e != nil {
				return nil, e
			}
			target := object
			for _, part := range parts[:len(parts)-1] {
				nested, ok := target[part].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("nested object %q does not exist", part)
				}
				target = nested
			}
			target[parts[len(parts)-1]] = value
			results = append(results, object)
		case "DELETE":
			deleted[i] = true
			results = append(results, nil)
		default:
			return nil, fmt.Errorf("unsupported mutation %q", change.Method)
		}
	}
	rows := make([]any, 0, len(values))
	kept := make([]string, 0, len(ids))
	for i, row := range values {
		if !deleted[i] {
			rows = append(rows, row)
			kept = append(kept, ids[i])
		}
	}
	b, err = conversion.Encode(rows, c.Format)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > s.MaxBytes {
		return nil, ErrTooLarge
	}
	for _, row := range rows {
		encoded, e := conversion.MarshalJSON(row)
		if e != nil {
			return nil, e
		}
		if int64(len(encoded)) > s.RecordBytes {
			return nil, fmt.Errorf("record exceeds size limit")
		}
	}
	// The mapping is content-addressed. A crash before replacing the source cannot
	// apply these identities to a different file; restart indexing recovers it.
	h = sha256.Sum256(b)
	digest := hex.EncodeToString(h[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM record_overrides WHERE collection_id=?`, id); err != nil {
		return nil, err
	}
	for i, rid := range kept {
		if _, err = tx.ExecContext(ctx, `INSERT INTO record_overrides(collection_id,source_sha256,ordinal,record_id) VALUES(?,?,?,?)`, id, digest, i, rid); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	// Recheck immediately before publishing; external writers are not locked by us.
	actual, err := sourceChecksum(ctx, c.Path)
	if err != nil {
		return nil, err
	}
	if actual != c.SourceSHA {
		return nil, ErrConflict
	}
	if err = atomicWrite(c.Path, b); err != nil {
		return nil, err
	}
	if err = s.syncFile(c.Path, true); err != nil {
		return nil, err
	}
	return results, nil
}
