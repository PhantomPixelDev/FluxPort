package jobs

import (
	"context"
	"fluxport/internal/database"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkersCancellationRestart(t *testing.T) {
	db, e := database.Open(filepath.Join(t.TempDir(), "state"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	m := New(db)
	var active, peak atomic.Int32
	m.Register("work", func(ctx context.Context, j Job, progress func(int64, int64)) (string, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		progress(10, 1)
		<-ctx.Done()
		return "", ctx.Err()
	})
	if e = m.Start(context.Background(), 2); e != nil {
		t.Fatal(e)
	}
	ids := []string{}
	for i := 0; i < 4; i++ {
		j, e := m.Submit("work", "", "Test", "", 100)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, j.ID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for active.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active.Load() != 2 || peak.Load() > 2 {
		t.Fatal("worker bound", active.Load(), peak.Load())
	}
	if e = m.Cancel(ids[3]); e != nil {
		t.Fatal(e)
	}
	j, _ := m.Get(ids[3])
	if j.State != "cancelled" {
		t.Fatal(j)
	}
	m.Stop()
	restarted := New(db)
	if e = restarted.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer restarted.Stop()
	for _, id := range ids[:3] {
		j, _ = restarted.Get(id)
		if j.State != "interrupted" {
			t.Fatal("restart state", j.State)
		}
	}
}
func TestPanicContained(t *testing.T) {
	_, err := invoke(func(context.Context, Job, func(int64, int64)) (string, error) { panic("broken parser") }, context.Background(), Job{}, func(int64, int64) {})
	if err == nil {
		t.Fatal("panic not converted to job error")
	}
}
func TestProgressOverflowCoalesces(t *testing.T) {
	m := New(nil)
	defer m.Stop()
	ch, close, e := m.Subscribe()
	if e != nil {
		t.Fatal(e)
	}
	defer close()
	for i := 0; i < 100; i++ {
		m.notify("job")
	}
	found := false
	for len(ch) > 0 {
		if <-ch == "*" {
			found = true
		}
	}
	if !found {
		t.Fatal("final updates could be silently lost")
	}
}
