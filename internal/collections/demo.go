package collections

import (
	"bytes"
	"fluxport/internal/conversion"
	"fluxport/internal/fixtures"
	"fmt"
	"os"
)

// Demo is explicit and idempotent; its setting survives removal of examples.
func (s *Service) Demo() error {
	unlock := s.lock("demo-pack")
	defer unlock()
	for i, format := range conversion.Formats {
		key := "demo_v1_" + format
		if s.Setting(key, "") != "" {
			continue
		}
		profile := []string{"users", "products", "orders", "events"}[i%4]
		var b bytes.Buffer
		w, err := conversion.NewRowWriter(&b, format, fixtures.Fields)
		if err != nil {
			return err
		}
		for n := int64(0); n < 100; n++ {
			if err = w.Write(fixtures.Record(profile, n, 42)); err != nil {
				return err
			}
		}
		if err = w.Close(); err != nil {
			return err
		}
		path, err := s.Stage(fmt.Sprintf("demo-%s.%s", profile, format), &b)
		if err != nil {
			return err
		}
		if err = s.SetSetting(key, path); err != nil {
			os.Remove(path)
			return err
		}
		if err = s.SyncFile(path); err != nil {
			return err
		}
	}
	return nil
}
