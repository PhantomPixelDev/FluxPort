package collections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestClipboardBoundsAndQuotes(t *testing.T) {
	exact, err := ParseClipboard("\"one\r\ntwo\"\t\"\"")
	if err != nil || exact[0][0] != "one\r\ntwo" || exact[0][1] != "" {
		t.Fatal(exact, err)
	}
	rows, err := ParseClipboard("\"山田\t太郎\"\t\"one\nline\"\r\n00123\t18446744073709551617\r\n")
	if err != nil || len(rows) != 2 || rows[0][0] != "山田\t太郎" || rows[0][1] != "one\nline" || rows[1][0] != "00123" {
		t.Fatal(rows, err)
	}
	for _, text := range []string{"a\tb\nc", "\"unclosed", strings.Repeat("a\n", 1001), strings.Repeat("a\t", 5000) + "a", strings.Repeat("x", 1<<20+1)} {
		if _, err = ParseClipboard(text); err == nil {
			t.Fatal("invalid paste accepted")
		}
	}
}
func TestFileCommandsAtomicHistoryAndLiteralColumns(t *testing.T) {
	s := testService(t)
	original := "[\n {\"a.b/x\":\"old\",\"name\":\"Ada\",\"n\":18446744073709551617,\"null\":null},\n {\"a.b/x\":\"next\",\"name\":\"Grace\",\"empty\":\"\"}\n]\n"
	c, err := s.Import("commands.json", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	c, q := fileRows(t, s, c.ID)
	ids := []string{q.Records[0].ID, q.Records[1].ID}
	cmd := FileCommand{Action: "paste", Generation: c.Generation, IDs: ids, Fields: []string{"a.b/x", "name"}, Text: "00123\t\"山田\n太郎\"\n18446744073709551617\tRenée"}
	p, err := s.FileCommand(c.ID, cmd, false)
	if err != nil || p.Cells != 4 || p.Rows != 2 {
		t.Fatal(p, err)
	}
	if b, _ := os.ReadFile(c.Path); string(b) != original {
		t.Fatal("preview changed source")
	}
	if _, err = s.FileCommand(c.ID, cmd, true); err != nil {
		t.Fatal(err)
	}
	c, rows := fileRows(t, s, c.ID)
	first := rows.Records[0].Value.(map[string]any)
	if first["a.b/x"] != "00123" || first["name"] != "山田\n太郎" || first["n"] != json.Number("18446744073709551617") {
		t.Fatal(first)
	}
	if _, ok := first["null"]; !ok || first["null"] != nil {
		t.Fatal("null changed")
	}
	if _, err = s.FileCommand(c.ID, cmd, true); !errors.Is(err, ErrConflict) {
		t.Fatal("stale paste allowed", err)
	}
	if err = s.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	if b, _ := os.ReadFile(c.Path); string(b) != original {
		t.Fatal("undo did not restore exact bytes")
	}
	// A later row error leaves the complete source unchanged.
	bad := FileCommand{Action: "paste", Generation: c.Generation, IDs: []string{ids[0], "missing"}, Fields: []string{"name"}, Text: "changed\nbad"}
	if _, err = s.FileCommand(c.ID, bad, true); err == nil {
		t.Fatal("bad row accepted")
	}
	if b, _ := os.ReadFile(c.Path); string(b) != original {
		t.Fatal("partial save")
	}
	for _, command := range []FileCommand{
		{Action: "add_column", Name: "new/key", Value: ""},
		{Action: "rename_column", Fields: []string{"a.b/x"}, Name: "renamed.dot"},
		{Action: "fill_down", IDs: ids, Fields: []string{"name"}},
		{Action: "find_replace", Fields: []string{"name"}, Find: "Ada", Replacement: "Grace", Scope: "selected", IDs: []string{ids[1]}},
		{Action: "delete_columns", Fields: []string{"new/key"}},
		{Action: "add_row", Value: map[string]any{"name": "new"}},
	} {
		c, _ = fileRows(t, s, c.ID)
		command.Generation = c.Generation
		if _, err = s.FileCommand(c.ID, command, true); err != nil {
			t.Fatal(command.Action, err)
		}
	}
	c, rows = fileRows(t, s, c.ID)
	if len(rows.Records) != 3 {
		t.Fatal(rows)
	}
	if _, err = s.FileCommand(c.ID, FileCommand{Generation: c.Generation, Action: "rename_column", Fields: []string{"name"}, Name: "n"}, true); err == nil {
		t.Fatal("column collision allowed")
	}
}
func TestFindReplaceAllMatchingAndEmptyArray(t *testing.T) {
	s := testService(t)
	c, err := s.Import("replace.json", strings.NewReader(`[{"name":"old a","group":"a"},{"name":"old b","group":"b"},{"name":"old c","group":"a"}]`))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	cmd := FileCommand{Generation: c.Generation, Action: "find_replace", Fields: []string{"name"}, Find: "old", Replacement: "new", Query: Query{Search: "\"a\"", Limit: 1}}
	// Use an exact filter, independent of page limits.
	cmd.Query = Query{Filters: []Filter{{Field: "group", Op: "eq", Value: "a"}}, Limit: 1}
	p, err := s.FileCommand(c.ID, cmd, true)
	if err != nil || p.Rows != 2 {
		t.Fatal(p, err)
	}
	_, q := fileRows(t, s, c.ID)
	if q.Records[1].Value.(map[string]any)["name"] != "old b" {
		t.Fatal("unmatched row changed")
	}
	empty, err := s.Import("empty.json", strings.NewReader("[]"))
	if err != nil {
		t.Fatal(err)
	}
	empty, _ = fileRows(t, s, empty.ID)
	if _, err = s.FileCommand(empty.ID, FileCommand{Generation: empty.Generation, Action: "paste", Append: true, Fields: []string{"code", "label"}, Text: "00123\tAda\n00234\tGrace"}, true); err != nil {
		t.Fatal(err)
	}
	_, q = fileRows(t, s, empty.ID)
	if len(q.Records) != 2 || q.Records[0].Value.(map[string]any)["code"] != "00123" {
		t.Fatal(q)
	}
}
func TestEditableCopyPreservesSourceAndBounds(t *testing.T) {
	s := testService(t)
	source := `[{"id":18446744073709551617,"name":"Ada"},{"id":2,"name":"Grace"}]`
	c, err := s.Import("source.json", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	job, err := s.EditableCopyJob(c.ID, c.Generation, Query{Search: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.editableCopyJob(context.Background(), job, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	copy, rows := fileRows(t, s, id)
	if copy.Name != "source — editable.json" || len(rows.Records) != 1 || rows.Records[0].Value.(map[string]any)["id"] != json.Number("18446744073709551617") {
		t.Fatal(copy, rows)
	}
	if b, _ := os.ReadFile(c.Path); string(b) != source {
		t.Fatal("source changed")
	}
	second, err := s.editableCopyJob(context.Background(), job, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.Get(second)
	if other.Name != "source — editable (2).json" {
		t.Fatal(other.Name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.editableCopyJob(ctx, job, func(int64, int64) {}); err == nil {
		t.Fatal("cancelled copy published")
	}
	s.MaxBytes = 10
	if _, err = s.editableCopyJob(context.Background(), job, func(int64, int64) {}); err == nil {
		t.Fatal("oversized copy allowed")
	}
	s.MaxBytes = 4096
	s.DiskCheck = func(string, uint64) error { return errors.New("low disk") }
	if _, err = s.editableCopyJob(context.Background(), job, func(int64, int64) {}); err == nil {
		t.Fatal("low disk ignored")
	}
}
