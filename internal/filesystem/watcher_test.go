package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/database"
)

func TestFilesystemEvents(t *testing.T) {
	root := t.TempDir()
	db, err := database.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := collections.New(db, filepath.Join(root, "data"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop, err := Watch(ctx, s)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); stop() }()
	path := filepath.Join(s.Root, "watched.json")
	if err = os.WriteFile(path, []byte(`[{"name":"Ada"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, _, err := s.List("", 1, 20)
		if err == nil && len(items) == 1 && items[0].Rows == 1 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("filesystem creation did not synchronize")
}
