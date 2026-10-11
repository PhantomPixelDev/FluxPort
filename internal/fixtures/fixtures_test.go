package fixtures

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReproducibleSuites(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if _, e := Generate(context.Background(), Options{Suite: "smoke", Out: dir, Seed: 42}); e != nil {
			t.Fatal(e)
		}
		if e := EdgeCases(dir); e != nil {
			t.Fatal(e)
		}
	}
	entries, e := os.ReadDir(a)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		x, _ := os.ReadFile(filepath.Join(a, entry.Name()))
		y, _ := os.ReadFile(filepath.Join(b, entry.Name()))
		sx, sy := sha256.Sum256(x), sha256.Sum256(y)
		if hex.EncodeToString(sx[:]) != hex.EncodeToString(sy[:]) {
			t.Fatalf("non-deterministic %s", entry.Name())
		}
	}
}
func TestStressRequiresSize(t *testing.T) {
	if _, e := Generate(context.Background(), Options{Suite: "stress", Out: t.TempDir(), Seed: 42}); e == nil {
		t.Fatal("stress without explicit size accepted")
	}
}
