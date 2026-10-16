package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
)

type editableCopyPayload struct {
	Generation string
	Query      Query
}
type copyLimitWriter struct {
	io.Writer
	size, max int64
}

func (w *copyLimitWriter) Write(b []byte) (int, error) {
	if w.size+int64(len(b)) > w.max {
		return 0, fmt.Errorf("editable copy exceeds %d bytes; use narrower filters", w.max)
	}
	n, e := w.Writer.Write(b)
	w.size += int64(n)
	return n, e
}
func (s *Service) EditableCopyJob(id, generation string, q Query) (jobs.Job, error) {
	c, err := s.Get(id)
	if err != nil {
		return jobs.Job{}, err
	}
	c, err = s.ready(c)
	if err != nil {
		return jobs.Job{}, err
	}
	if generation == "" || c.Generation != generation {
		return jobs.Job{}, ErrConflict
	}
	q = NormalizeInputQuery(q)
	if err = ValidateQuery(q); err != nil {
		return jobs.Job{}, err
	}
	payload, err := json.Marshal(editableCopyPayload{generation, q})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("editable_copy", id, "Editable copy of "+c.Name, string(payload), c.Size)
}
func (s *Service) editableCopyJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var p editableCopyPayload
	if err := json.Unmarshal([]byte(j.Payload), &p); err != nil {
		return "", err
	}
	c, err := s.Get(j.Resource)
	if err != nil {
		return "", err
	}
	ref, err := s.sourceReference(c, p.Generation)
	if err != nil {
		return "", err
	}
	defer ref.release()
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.DownloadDir, ".editable-copy-"+j.ID+"-*.json")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	bounded := &copyLimitWriter{Writer: f, max: s.MaxBytes}
	writer, err := conversion.NewRowWriter(bounded, "json", nil)
	if err != nil {
		return "", err
	}
	count := int64(0)
	lastCheck := int64(0)
	err = s.eachReference(ctx, ref, p.Query, func(row any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 {
			return fmt.Errorf("editable copies support 100000 rows; use narrower filters")
		}
		payload, e := conversion.MarshalJSON(row)
		if e != nil {
			return e
		}
		if int64(len(payload)) > s.RecordBytes {
			return fmt.Errorf("record exceeds size limit")
		}
		if bounded.size-lastCheck >= 4<<20 {
			if e = s.DiskCheck(s.DownloadDir, s.Reserve); e != nil {
				return e
			}
			lastCheck = bounded.size
		}
		if e = writer.Write(row); e != nil {
			return e
		}
		progress(min(c.Size, count*c.Size/max(int64(1), ref.rows)), count)
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err = s.checkResultGenerations(map[string]string{c.ID: p.Generation}); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	// Import only after the complete bounded snapshot has been generated.
	copy, err := s.Import("editable.json", f)
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		if !success {
			_ = s.Delete(copy.ID)
		}
	}()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	unlock := s.lock("editable-copy-display-names")
	defer unlock()
	stem := strings.TrimSuffix(c.Name, filepath.Ext(c.Name))
	if len(stem) > 100 {
		stem = string([]rune(stem)[:min(60, len([]rune(stem)))])
	}
	name := stem + " — editable.json"
	for suffix := 2; ; suffix++ {
		var n int
		err = s.DB.QueryRow("SELECT COUNT(*) FROM collections WHERE CASE WHEN display_name='' THEN name ELSE display_name END=?", name).Scan(&n)
		if err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		name = fmt.Sprintf("%s — editable (%d).json", stem, suffix)
	}
	if err = s.Rename(copy.ID, name); err != nil {
		return "", err
	}
	success = true
	progress(c.Size, count)
	return copy.ID, nil
}
