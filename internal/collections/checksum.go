package collections

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

type checkedReader struct {
	ctx context.Context
	r   io.Reader
}

func (r checkedReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func sourceChecksum(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.CopyBuffer(h, checkedReader{ctx: ctx, r: f}, make([]byte, 64<<10)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) Invalidate(path string) error {
	c, err := s.byPath(path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`UPDATE collections SET status='pending',job_id='' WHERE id=? AND job_id=?`, c.ID, c.JobID)
	if err != nil {
		return err
	}
	if c.JobID != "" {
		return s.Jobs.Cancel(c.JobID)
	}
	return nil
}
func (s *Service) indexPolicy(format string) string {
	return fmt.Sprintf("v3:%s:%d:%d", format, s.RecordBytes, s.MaxBytes)
}
