package collections

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxport/internal/database"
)

func testService(t testing.TB) *Service {
	t.Helper()
	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := New(db, filepath.Join(root, "data"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestImportSyncPersistentConfiguration(t *testing.T) {
	s := testService(t)
	c, err := s.Import("../people.json", strings.NewReader(`[{"name":"Ada"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Rows != 1 || filepath.Dir(c.Path) != s.Root {
		t.Fatal(c)
	}
	if err = s.Update(c.ID, false, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.Path, []byte(`[{"name":"Ada"},{"name":"Grace"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(c.ID)
	if err != nil || got.Rows != 2 || got.Enabled || !got.CRUD {
		t.Fatalf("%+v %v", got, err)
	}
	if err = os.Remove(c.Path); err != nil {
		t.Fatal(err)
	}
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	_, total, err := s.List("", 1, 20)
	if err != nil || total != 0 {
		t.Fatalf("%d %v", total, err)
	}
}
func TestBoundsAndParseErrors(t *testing.T) {
	s := testService(t)
	if _, err := s.Import("big.json", strings.NewReader(strings.Repeat(" ", 1025))); err == nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root, "broken.json")
	if err := os.WriteFile(path, []byte(`{"broken":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.List("", 1, 20)
	if err != nil || len(rows) != 1 || rows[0].Error == "" {
		t.Fatalf("%+v %v", rows, err)
	}
	outside := filepath.Join(filepath.Dir(s.Root), "secret.json")
	os.WriteFile(outside, []byte(`[]`), 0600)
	if _, err = s.Read(Collection{Path: outside, Format: "json"}); err == nil {
		t.Fatal("read outside root")
	}
}
func TestCRUDAtomicAndSettings(t *testing.T) {
	s := testService(t)
	c, err := s.Import("people.json", strings.NewReader(`[{"name":"Ada","delete":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(c.ID, "POST", 0, map[string]any{"name": "Grace"}); err == nil {
		t.Fatal("CRUD allowed by default")
	}
	if err = s.Update(c.ID, true, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(c.ID, "POST", 0, map[string]any{"name": "Grace"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Mutate(c.ID, "PATCH", 0, map[string]any{"name": "Updated", "delete": nil}); err != nil {
		t.Fatal(err)
	}
	v, err := s.Read(c)
	if err != nil {
		t.Fatal(err)
	}
	if v.([]any)[0].(map[string]any)["name"] != "Updated" {
		t.Fatal(v)
	}
	if _, err = s.Mutate(c.ID, "DELETE", 1, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSetting("watch_enabled", "false"); err != nil {
		t.Fatal(err)
	}
	if s.Setting("watch_enabled", "true") != "false" {
		t.Fatal("settings not saved")
	}
	files, _ := os.ReadDir(s.Root)
	if len(files) != 1 {
		t.Fatal("temporary files leaked")
	}
}
