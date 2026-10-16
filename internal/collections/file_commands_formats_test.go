package collections

import (
	"context"
	"errors"
	"fluxport/internal/conversion"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileCommandsAcrossTextFormats(t *testing.T) {
	for _, format := range []string{"json", "csv", "ndjson", "yaml", "xml", "toml"} {
		t.Run(format, func(t *testing.T) {
			s := testService(t)
			var value any = []any{map[string]any{"name": "Ada", "note": "old"}, map[string]any{"name": "Grace", "note": ""}}
			if format == "toml" || format == "xml" {
				value = map[string]any{"name": "Ada", "note": "old"}
			}
			data, err := conversion.Encode(value, format)
			if err != nil {
				t.Fatal(err)
			}
			c, err := s.Import("source."+format, strings.NewReader(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			c, _ = fileRows(t, s, c.ID)
			if _, err = s.FileCommand(c.ID, FileCommand{Generation: c.Generation, Action: "add_column", Name: "new/key.dot", Value: "00123"}, true); err != nil {
				t.Fatal(err)
			}
			c, _ = fileRows(t, s, c.ID)
			if err = s.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
				t.Fatal(err)
			}
			if after, _ := os.ReadFile(c.Path); string(after) != string(data) {
				t.Fatal("Undo changed exact bytes")
			}
		})
	}
}
func TestEditableCopySourceChangeRecordCapAndAbandonedFiles(t *testing.T) {
	s := testService(t)
	c, err := s.Import("changing.json", strings.NewReader(`[{"name":"Ada"},{"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	job, err := s.EditableCopyJob(c.ID, c.Generation, Query{})
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	if _, err = s.editableCopyJob(context.Background(), job, func(int64, int64) {
		if !changed {
			changed = true
			if e := os.WriteFile(c.Path, []byte(`[{"name":"External"}]`), 0600); e != nil {
				t.Fatal(e)
			}
			if e := s.SyncFile(c.Path); e != nil {
				t.Fatal(e)
			}
		}
	}); !errors.Is(err, ErrConflict) {
		t.Fatal("changed source published", err)
	}
	var copies int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM collections WHERE display_name LIKE '%editable%'").Scan(&copies); err != nil || copies != 0 {
		t.Fatal(copies, err)
	}
	abandoned := filepath.Join(s.DownloadDir, ".editable-copy-000000000000000000000000-part.json")
	if err = os.WriteFile(abandoned, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	s.cleanupDownloads()
	if _, err = os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatal("abandoned partial retained")
	}
	s.MaxBytes = 1 << 20
	large, err := s.Import("record-cap.ndjson", strings.NewReader(strings.Repeat("null\n", 100001)))
	if err != nil {
		t.Fatal(err)
	}
	large, err = s.ready(large)
	if err != nil {
		t.Fatal(err)
	}
	largeJob, err := s.EditableCopyJob(large.ID, large.Generation, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.editableCopyJob(context.Background(), largeJob, func(int64, int64) {}); err == nil || !strings.Contains(err.Error(), "100000") {
		t.Fatal("record cap ignored", err)
	}
}
