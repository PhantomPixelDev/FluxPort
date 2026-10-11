package collections

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) safe(path string) error {
	rel, err := filepath.Rel(s.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path is outside data directory")
	}
	current := s.Root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		stat, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported")
		}
	}
	return nil
}
func (s *Service) read(path string) ([]byte, error) {
	if err := s.safe(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > s.MaxBytes {
		return nil, ErrTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(f, s.MaxBytes+1))
	if int64(len(b)) > s.MaxBytes {
		return nil, ErrTooLarge
	}
	return b, err
}
