package collections

import (
	"database/sql"
	"fluxport/internal/conversion"
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

// Mutate rewrites the source atomically. Row indexes refer to the untransformed array.
func (s *Service) Mutate(id, method string, index int, value any) (any, error) {
	c0, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	unlock := s.lock(c0.Path)
	defer unlock()
	c, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if c.ReadOnly {
		return nil, fmt.Errorf("large collections are read-only")
	}
	if !c.Enabled || !c.CRUD {
		return nil, fmt.Errorf("CRUD is disabled")
	}
	if c.Rules != "" && c.Rules != "{}" {
		return nil, fmt.Errorf("remove transformations before enabling writes")
	}
	b, err := s.read(c.Path)
	if err != nil {
		return nil, err
	}
	v, err := conversion.Decode(b, c.Format)
	if err != nil {
		return nil, err
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("CRUD requires an array at the source root")
	}
	if method != "POST" && (index < 0 || index >= len(rows)) {
		return nil, sql.ErrNoRows
	}
	var result any
	switch method {
	case "POST":
		rows = append(rows, value)
		result = value
	case "PUT":
		rows[index] = value
		result = value
	case "PATCH":
		obj, ok := rows[index].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("PATCH requires an object row")
		}
		patch, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("PATCH body must be an object")
		}
		for k, v := range patch {
			if v == nil {
				delete(obj, k)
			} else {
				obj[k] = v
			}
		}
		result = obj
	case "DELETE":
		rows = append(rows[:index], rows[index+1:]...)
	default:
		return nil, fmt.Errorf("unsupported mutation")
	}
	b, err = conversion.Encode(rows, c.Format)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > s.MaxBytes {
		return nil, ErrTooLarge
	}
	if err = atomicWrite(c.Path, b); err != nil {
		return nil, err
	}
	if err = s.syncFile(c.Path, true); err != nil {
		return nil, err
	}
	return result, nil
}
