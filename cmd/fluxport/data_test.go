package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxport/internal/collections"
	"fluxport/internal/config"
	"fluxport/internal/database"
)

func TestDataCommandsSharedService(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLUXPORT_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("FLUXPORT_STATE_DIR", filepath.Join(root, "state"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := collections.New(db, cfg.DataDir, cfg.MaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Import("input.json", strings.NewReader(`[{"id":1,"name":"Ada"}]`))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"inspect", c.ID}, {"validate", c.ID}, {"query", "--collections", c.ID, "--sql", "SELECT COUNT(*) FROM c_" + c.ID, "--engine", "duckdb"}, {"pipeline", "list"}, {"export", c.ID, "--format", "parquet", "--output", filepath.Join(root, "out.parquet")}} {
		if err = run(args); err != nil {
			t.Fatal(args, err)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "out.parquet")); err != nil || info.Size() == 0 {
		t.Fatal(info, err)
	}
	if err = run([]string{"export", c.ID, "--output", c.Path}); err == nil {
		t.Fatal("source overwrite allowed")
	}
}
