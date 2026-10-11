package collections

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// ConversionInput snapshots the source, preserving document roots and CSV
// options. Only a snapshot matching the indexed source is allowed to proceed.
func (s *Service) ConversionInput(id string) (string, Collection, error) {
	c, err := s.Get(id)
	if err != nil {
		return "", c, err
	}
	c, err = s.ready(c)
	if err != nil {
		return "", c, err
	}
	source, err := s.RawSource(c)
	if err != nil {
		return "", c, err
	}
	defer source.Close()
	hash := sha256.New()
	path, err := s.StageConversion(c.Filename, io.TeeReader(source, hash))
	if err != nil {
		return "", c, err
	}
	if hex.EncodeToString(hash.Sum(nil)) != c.SourceSHA {
		os.Remove(path)
		return "", c, fmt.Errorf("source changed; refresh this collection before converting")
	}
	return path, c, nil
}
