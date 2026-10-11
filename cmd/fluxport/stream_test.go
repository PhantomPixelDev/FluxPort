package main

import (
	"fluxport/internal/conversion"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamingStdinConversion(t *testing.T) {
	t.Setenv("FLUXPORT_MAX_FILE_BYTES", "1024")
	t.Setenv("FLUXPORT_MIN_FREE_DISK_BYTES", "0")
	dir := t.TempDir()
	input := filepath.Join(dir, "input.ndjson")
	os.WriteFile(input, []byte(strings.Repeat("{\"id\":9007199254740993}\n", 200)), 0600)
	f, e := os.Open(input)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	previous := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = previous }()
	output := filepath.Join(dir, "out.json")
	if e = run([]string{"convert", "-", "--from", "ndjson", "--to", "json", "--output", output}); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	v, e := conversion.Decode(b, "json")
	if e != nil || len(conversion.Rows(v)) != 200 {
		t.Fatal(e)
	}
}
