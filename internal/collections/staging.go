package collections

import (
	"io"
	"os"
	"path/filepath"
)

func (s *Service) StageConversion(name string, r io.Reader) (string, error) {
	path, err := s.Stage(name, r)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.Root, ".conversion-inputs")
	if err = os.MkdirAll(dir, 0700); err != nil {
		os.Remove(path)
		return "", err
	}
	final := filepath.Join(dir, filepath.Base(path))
	if err = os.Rename(path, final); err != nil {
		os.Remove(path)
		return "", err
	}
	return final, nil
}
