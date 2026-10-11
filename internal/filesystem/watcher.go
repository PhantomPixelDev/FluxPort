package filesystem

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fluxport/internal/collections"
	"github.com/fsnotify/fsnotify"
)

// Watch uses one event loop and a single debounce timer; there is no polling.
func Watch(ctx context.Context, s *collections.Service) (func(), error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	add := func(root string) error {
		known := map[string]bool{}
		for _, path := range w.WatchList() {
			known[path] = true
		}
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != s.Root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() && !known[path] {
				return w.Add(path)
			}
			return nil
		})
	}
	if err = add(s.Root); err != nil {
		w.Close()
		return nil, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		defer timer.Stop()
		directories := map[string]bool{}
		for _, p := range w.WatchList() {
			directories[p] = true
		}
		var tick <-chan time.Time
		pending := map[string]bool{}
		reconcile := false
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				info, statErr := os.Stat(event.Name)
				if statErr == nil && info.IsDir() && event.Has(fsnotify.Create) && !strings.HasPrefix(filepath.Base(event.Name), ".") {
					directories[event.Name] = true
					if err := add(event.Name); err != nil {
						slog.Warn("watch directories", "error", err)
					}
					filepath.WalkDir(event.Name, func(path string, d fs.DirEntry, err error) error {
						if err == nil && !d.IsDir() {
							if !pending[path] {
								pending[path] = false
							}
						}
						return nil
					})
				}
				if s.Setting("watch_enabled", "true") == "true" {
					if !strings.HasPrefix(filepath.Base(event.Name), ".") {
						pending[event.Name] = pending[event.Name] || event.Has(fsnotify.Write)
						if event.Has(fsnotify.Write) {
							if err := s.Invalidate(event.Name); err != nil {
								slog.Warn("invalidate changed source", "error", err)
							}
						}
					}
					if len(pending) > 1024 {
						pending = map[string]bool{}
						reconcile = true
					}
					timer.Reset(250 * time.Millisecond)
					tick = timer.C
				}
			case <-tick:
				tick = nil
				if reconcile {
					if err := s.Sync(); err != nil {
						slog.Error("filesystem sync", "error", err)
					}
				} else {
					for path, force := range pending {
						var err error
						if directories[path] {
							err = s.ReconcileSubtree(path)
						} else if force {
							err = s.SyncFile(path)
						} else {
							err = s.SyncChangedFile(path)
						}
						if err != nil {
							slog.Warn("refresh source", "path", path, "error", err)
						}
					}
				}
				pending = map[string]bool{}
				reconcile = false
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				slog.Error("filesystem watcher", "error", err)
				reconcile = true
				timer.Reset(250 * time.Millisecond)
				tick = timer.C
			}
		}
	}()
	return func() { w.Close(); wg.Wait() }, nil
}
