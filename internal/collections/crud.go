package collections

import (
	"database/sql"
	"fluxport/internal/transformations"
	"fmt"
	"os"
	"path/filepath"
)

func atomicWrite(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".fluxport-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (s *Service) Update(id string, enabled, crud bool, rules string) error {
	if crud {
		c, err := s.Get(id)
		if err != nil {
			return err
		}
		if c.Size > s.MaxBytes {
			return fmt.Errorf("large collections are read-only; CRUD is limited to %d bytes", s.MaxBytes)
		}
	}
	if _, err := transformations.Parse(rules); err != nil {
		return err
	}
	result, err := s.DB.Exec(`UPDATE collections SET enabled=?,crud=?,rules=? WHERE id=?`, enabled, crud, rules, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}

// Mutate preserves the existing ordinal-based API. New clients use BulkMutate
// with stable record IDs and the generation shown in their preview.
func (s *Service) Mutate(id, method string, index int, value any) (any, error) {
	results, err := s.mutateRecords(id, "", []Mutation{{Method: method, Ordinal: index, Value: value}}, false)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}
