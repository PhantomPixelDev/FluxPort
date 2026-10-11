package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fluxport/internal/conversion"
)

func TestCLIConversionOptionsSmallAndStreaming(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "types.csv")
	output := filepath.Join(dir, "types.json")
	data := "id;amount;active\n"
	for i := 0; i < 100; i++ {
		data += "00123;2.50;true\n"
	}
	if e := os.WriteFile(input, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	for _, limit := range []string{"16777216", "1024"} {
		t.Setenv("FLUXPORT_MAX_FILE_BYTES", limit)
		if e := run([]string{"convert", input, "--to", "json", "--delimiter", "auto", "--infer-types", "--output", output}); e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(output)
		if e != nil {
			t.Fatal(e)
		}
		v, e := conversion.Decode(b, "json")
		if e != nil {
			t.Fatal(e)
		}
		row := conversion.Rows(v)[0].(map[string]any)
		if row["id"] != "00123" || row["amount"] != json.Number("2.50") || row["active"] != true {
			t.Fatal(row)
		}
	}
}

func TestCLIConvert(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.json")
	output := filepath.Join(dir, "output.xml")
	if err := os.WriteFile(input, []byte(`[{"name":"Ada"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"convert", input, "--to", "xml", "--output", output}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conversion.Decode(b, "xml"); err != nil {
		t.Fatal(err)
	}
	if err = run([]string{"convert", input, "--to", "json", "--output", input}); err == nil {
		t.Fatal("input overwrite allowed")
	}
	if err = run([]string{"unknown"}); err == nil {
		t.Fatal("unknown command accepted")
	}
}
