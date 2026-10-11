package collections

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestConversionSnapshotPreservesRootAndRejectsChangedSource(t *testing.T) {
	s := testService(t)
	input := `{"customer":{"name":"Ada","address":{"city":"Berlin"}},"orders":[{"id":1}]}`
	c, err := s.Import("nested.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	path, _, err := s.ConversionInput(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != input {
		t.Fatal("snapshot changed root or values", err)
	}
	info, _ := os.Stat(c.Path)
	changed := strings.Replace(input, "Ada", "Eve", 1)
	if err = os.WriteFile(c.Path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(c.Path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.ConversionInput(c.ID); err == nil {
		t.Fatal("changed contents accepted with preserved timestamp")
	}
}

func TestSourceSearchIgnoresAPIRulesAndPaginates(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1,"name":"Ada"},{"id":2,"name":"Grace"},{"id":3,"name":"ADA"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(c.ID, true, false, `{"filter":{"name":"Grace"}}`); err != nil {
		t.Fatal(err)
	}
	rows, total, err := s.Page(context.Background(), c, 2, 1, false, "ada")
	if err != nil || total != 2 || len(rows) != 1 || rows[0].(map[string]any)["name"] != "ADA" {
		t.Fatal(rows, total, err)
	}
	rows, total, err = s.Page(context.Background(), c, 1, 20, true, "ada")
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("API filtering changed", rows, total, err)
	}
}
