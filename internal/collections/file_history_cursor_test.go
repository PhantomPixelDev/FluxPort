package collections

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestHistoryCursorSurvivesIndexRefreshButRejectsExternalBytes(t *testing.T) {
	s := testService(t)
	source := `[{"name":"Ada"}]`
	c, err := s.Import("cursor.json", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	if _, err = s.FileCommand(c.ID, FileCommand{Generation: c.Generation, Action: "add_column", Name: "new", Value: "text"}, true); err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	h, err := s.FileHistory(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RestoreFile(c.ID, "earlier-index-generation", "undo", 0, h.Current); err != nil {
		t.Fatal("unchanged source rejected", err)
	}
	if b, _ := os.ReadFile(c.Path); string(b) != source {
		t.Fatal("incorrect Undo bytes")
	}
	c, _ = fileRows(t, s, c.ID)
	h, err = s.FileHistory(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.Path, []byte(`[{"name":"External"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.RestoreFile(c.ID, "earlier-index-generation", "redo", 0, h.Current); !errors.Is(err, ErrConflict) {
		t.Fatal("external source overwritten", err)
	}
}
