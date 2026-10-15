package collections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fileRows(t *testing.T, s *Service, id string) (Collection, QueryResult) {
	t.Helper()
	c, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Query(context.Background(), c, Query{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return c, rows
}

func TestFileDeleteRemovesHistory(t *testing.T) {
	s := testService(t)
	c, err := s.Import("remove-history.json", strings.NewReader("[1]"))
	if err != nil {
		t.Fatal(err)
	}
	c, rows := fileRows(t, s, c.ID)
	if _, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "PUT", Value: "saved"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(s.historyFolder(c.ID)); !os.IsNotExist(err) {
		t.Fatal("snapshot folder retained", err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM file_revisions WHERE collection_id=?", c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
func TestFileEditHistoryExactBytesAndAPIPermissions(t *testing.T) {
	s := testService(t)
	original := "[\n {\"name\":\"Ada\",\"a.b/x\":\"山田\",\"big\":18446744073709551617,\"empty\":\"\",\"nil\":null},\n {\"name\":\"Grace\"}\n]\n"
	c, err := s.Import("editable.json", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(c.ID, false, false, "{}"); err != nil {
		t.Fatal(err)
	}
	c, before := fileRows(t, s, c.ID)
	_, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: before.Records[0].ID, Method: "FIELDS", Value: []any{map[string]any{"field": "/name", "value": "新名"}, map[string]any{"field": "/a.b~1x", "value": ""}, map[string]any{"field": "/nil", "value": nil}}}})
	if err != nil {
		t.Fatal(err)
	}
	c, after := fileRows(t, s, c.ID)
	if c.Enabled || c.CRUD {
		t.Fatal("file editing enabled REST writes")
	}
	if after.Records[0].ID != before.Records[0].ID {
		t.Fatal("record identity changed")
	}
	value := after.Records[0].Value.(map[string]any)
	if value["big"].(json.Number).String() != "18446744073709551617" || value["name"] != "新名" || value["a.b/x"] != "" {
		t.Fatal(value)
	}
	if _, ok := value["nil"]; !ok || value["nil"] != nil {
		t.Fatal("null was deleted")
	}
	if _, err = s.BulkMutate(c.ID, c.Generation, []Mutation{{ID: after.Records[0].ID, Method: "DELETE"}}); err == nil {
		t.Fatal("REST flags ignored")
	}
	h, err := s.FileHistory(c.ID)
	if err != nil || len(h.Revisions) != 2 || h.Previous == 0 {
		t.Fatal(h, err)
	}
	saved, _ := os.ReadFile(c.Path)
	if err = s.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
		t.Fatal(err)
	}
	c, undone := fileRows(t, s, c.ID)
	b, _ := os.ReadFile(c.Path)
	if string(b) != original || undone.Records[0].ID != before.Records[0].ID {
		t.Fatal("Undo did not restore exact source and identity")
	}
	if err = s.RestoreFile(c.ID, c.Generation, "redo", 0); err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	b, _ = os.ReadFile(c.Path)
	if string(b) != string(saved) {
		t.Fatal("Redo did not restore saved bytes")
	}
	if _, err = s.EditFile(c.ID, before.Generation, []Mutation{{ID: before.Records[0].ID, Method: "DELETE"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit accepted", err)
	}
}
func TestFileHistoryBranchAndRestartRecovery(t *testing.T) {
	s := testService(t)
	c, err := s.Import("history.json", strings.NewReader("[{\"n\":1}]"))
	if err != nil {
		t.Fatal(err)
	}
	c, rows := fileRows(t, s, c.ID)
	before, _ := os.ReadFile(c.Path)
	ids := []string{rows.Records[0].ID}
	after := []byte("[{\"n\":2}]")
	if _, err = s.beginFileRevision(c, before, after, ids, ids, "pending edit"); err != nil {
		t.Fatal(err)
	}
	if err = s.recoverFileWrites(); err != nil {
		t.Fatal(err)
	}
	h, err := s.FileHistory(c.ID)
	if err != nil || len(h.Revisions) != 1 {
		t.Fatal("unfinished write was published", h, err)
	}
	if _, err = s.beginFileRevision(c, before, after, ids, ids, "saved edit"); err != nil {
		t.Fatal(err)
	}
	if err = atomicWrite(c.Path, after); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(s.DB, s.Root, s.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.recoverFileWrites(); err != nil {
		t.Fatal(err)
	}
	if err = restarted.SyncFile(c.Path); err != nil {
		t.Fatal(err)
	}
	c, rows = fileRows(t, restarted, c.ID)
	if rows.Records[0].ID != ids[0] {
		t.Fatal("recovery lost identity")
	}
	h, err = restarted.FileHistory(c.ID)
	if err != nil || len(h.Revisions) != 2 || h.Previous == 0 {
		t.Fatal(h, err)
	}
	if err = restarted.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
		t.Fatal(err)
	}
	c, rows = fileRows(t, restarted, c.ID)
	_, err = restarted.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "FIELD", Value: map[string]any{"field": "/n", "value": json.Number("3")}}})
	if err != nil {
		t.Fatal(err)
	}
	h, err = restarted.FileHistory(c.ID)
	if err != nil || h.Next != 0 || len(h.Revisions) != 2 {
		t.Fatal("new edit retained abandoned redo branch", h, err)
	}
}
func TestFileEditsDocumentAndFailuresKeepSource(t *testing.T) {
	s := testService(t)
	c, err := s.Import("document.json", strings.NewReader("{\"name\":\"Ada\",\"nested\":{\"n\":2}}"))
	if err != nil {
		t.Fatal(err)
	}
	c, rows := fileRows(t, s, c.ID)
	_, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "FIELD", Value: map[string]any{"field": "/nested/n", "value": json.Number("9007199254740993")}}})
	if err != nil {
		t.Fatal(err)
	}
	c, rows = fileRows(t, s, c.ID)
	b, _ := os.ReadFile(c.Path)
	if len(b) == 0 || b[0] != '{' || !strings.Contains(string(b), "9007199254740993") {
		t.Fatal("document shape or number changed", string(b))
	}
	saved := string(b)
	s.DiskCheck = func(string, uint64) error { return errors.New("not enough free disk") }
	if _, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "DELETE"}}); err == nil {
		t.Fatal("document deletion accepted")
	}
	if _, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "FIELD", Value: map[string]any{"field": "/name", "value": "Changed"}}}); err == nil {
		t.Fatal("low disk accepted")
	}
	b, _ = os.ReadFile(c.Path)
	if string(b) != saved {
		t.Fatal("failed edit changed source")
	}
	if err = s.RestoreFile(c.ID, c.Generation, "restore", 999999); err == nil {
		t.Fatal("invalid revision accepted")
	}
}
func TestFileHistoryRetention(t *testing.T) {
	s := testService(t)
	c, err := s.Import("retention.json", strings.NewReader("[{\"n\":0}]"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 23; i++ {
		c, rows := fileRows(t, s, c.ID)
		if _, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "FIELD", Value: map[string]any{"field": "/n", "value": i}}}); err != nil {
			t.Fatal(err)
		}
	}
	h, err := s.FileHistory(c.ID)
	if err != nil || len(h.Revisions) != fileHistoryCount || h.Previous == 0 {
		t.Fatal(h, err)
	}
	files, err := os.ReadDir(s.historyFolder(c.ID))
	if err != nil || len(files) != fileHistoryCount {
		t.Fatal("pruned snapshots remain", len(files), err)
	}
}
