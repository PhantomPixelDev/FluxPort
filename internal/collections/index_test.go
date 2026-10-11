package collections

import (
	"context"
	"errors"
	"fluxport/internal/jobs"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexedPaginationExclusionRestore(t *testing.T) {
	s := testService(t)
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 5000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"User %d"}`, i, i)
	}
	b.WriteByte(']')
	c, e := s.Import("users.json", strings.NewReader(b.String()))
	if e != nil {
		t.Fatal(e)
	}
	if c.Rows != 5000 || c.Status != "ready" {
		t.Fatal(c)
	}
	rows, total, e := s.Page(context.Background(), c, 250, 20, false, "")
	if e != nil || len(rows) != 20 || total != 5000 || fmt.Sprint(rows[0].(map[string]any)["id"]) != "4980" {
		t.Fatalf("pagination: %d %d %v", len(rows), total, e)
	}
	if e = s.Update(c.ID, true, true, ""); e == nil {
		t.Fatal("large CRUD enabled")
	}
	if e = s.Rename(c.ID, "People"); e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	c, e = s.Get(c.ID)
	if e != nil || c.Name != "People" {
		t.Fatal("display name lost")
	}
	if e = s.Remove(c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(c.Path); e != nil {
		t.Fatal("source removed")
	}
	s.Sync()
	_, n, e := s.List("", 1, 20)
	if e != nil || n != 0 {
		t.Fatal("exclusion rediscovered")
	}
	excluded, _ := s.Excluded()
	if len(excluded) != 1 {
		t.Fatal(excluded)
	}
	if e = s.Restore(excluded[0].Key); e != nil {
		t.Fatal(e)
	}
	_, n, e = s.List("", 1, 20)
	if e != nil || n != 1 {
		t.Fatal("restore failed")
	}
}
func TestSourceChangeDuringIndexAndDiskFailure(t *testing.T) {
	s := testService(t)
	c, e := s.Import("events.ndjson", strings.NewReader("{\"id\":1}\n"))
	if e != nil {
		t.Fatal(e)
	}
	var b strings.Builder
	for i := 0; i < 2001; i++ {
		fmt.Fprintf(&b, "{\"id\":%d}\n", i)
	}
	os.WriteFile(c.Path, []byte(b.String()), 0600)
	checks := 0
	s.DiskCheck = func(string, uint64) error {
		checks++
		if checks == 2 {
			return os.WriteFile(c.Path, []byte("{\"id\":99}\n"), 0600)
		}
		return nil
	}
	if e = s.SyncFile(c.Path); e != nil {
		t.Fatal(e)
	}
	changed, _ := s.Get(c.ID)
	if changed.Status != "failed" {
		t.Fatal("changed source published", changed.Status)
	}
	if _, _, e = s.Page(context.Background(), c, 1, 20, false, ""); e == nil {
		t.Fatal("served outdated index")
	}
	s.DiskCheck = func(string, uint64) error { return errors.New("insufficient free disk") }
	s.SyncFile(c.Path)
	changed, _ = s.Get(c.ID)
	if !strings.Contains(changed.Error, "disk") {
		t.Fatal(changed)
	}
	raw, _ := os.ReadFile(c.Path)
	if string(raw) != "{\"id\":99}\n" {
		t.Fatal("source corrupted")
	}
}
func TestBackgroundCancellationAndRecovery(t *testing.T) {
	s := testService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	s.Jobs.Register("block", func(ctx context.Context, _ jobs.Job, _ func(int64, int64)) (string, error) {
		close(started)
		select {
		case <-release:
			return "", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	if e := s.Start(ctx, 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Stop)
	s.Jobs.Submit("block", "", "Block", "", 0)
	<-started
	c, e := s.Import("pending.json", strings.NewReader(`[{"id":1}]`))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Page(ctx, c, 1, 20, false, ""); !errors.Is(e, ErrPending) {
		t.Fatal(e)
	}
	if e = s.Jobs.Cancel(c.JobID); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "cancelled")
	close(release)
	if _, e = s.RetryJob(c.JobID); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
}
func TestDemoIdempotent(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 16 << 20
	if e := s.Demo(); e != nil {
		t.Fatal(e)
	}
	if e := s.Demo(); e != nil {
		t.Fatal(e)
	}
	_, n, e := s.List("", 1, 20)
	if e != nil || n != 6 {
		t.Fatalf("%d %v", n, e)
	}
}

func TestRestartRecoveryAndDisposableIndex(t *testing.T) {
	s := testService(t)
	c, e := s.Import("users.json", strings.NewReader(`[{"id":1}]`))
	if e != nil {
		t.Fatal(e)
	}
	s.DB.Exec(`UPDATE collections SET status='indexing' WHERE id=?`, c.ID)
	s.DB.Exec(`UPDATE jobs SET state='running' WHERE id=?`, c.JobID)
	if e = s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	got, _ := s.Get(c.ID)
	if got.Status != "interrupted" {
		t.Fatal(got.Status)
	}
	old, _ := s.Jobs.Get(c.JobID)
	if old.State != "interrupted" {
		t.Fatal(old.State)
	}
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	got, _ = s.Get(c.ID)
	if e = os.Remove(filepath.Join(s.IndexDir, got.Generation)); e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
}

func TestChecksumDetectsPreservedTimestamps(t *testing.T) {
	s := testService(t)
	c, e := s.Import("users.json", strings.NewReader(`[{"id":1}]`))
	if e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(c.Path)
	if e = os.WriteFile(c.Path, []byte(`[{"id":2}]`), 0600); e != nil {
		t.Fatal(e)
	}
	os.Chtimes(c.Path, info.ModTime(), info.ModTime())
	if e = s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	rows, _, e := s.Page(context.Background(), c, 1, 20, false, "")
	if e != nil || fmt.Sprint(rows[0].(map[string]any)["id"]) != "2" {
		t.Fatal("stale startup index", e)
	}
	updated, _ := s.Get(c.ID)
	if updated.Generation == c.Generation {
		t.Fatal("changed source reused index")
	}
	if e = s.SyncFile(c.Path); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	verified, _ := s.Get(c.ID)
	if verified.Generation != updated.Generation {
		t.Fatal("unchanged validated source was reparsed")
	}
}
func TestChecksumChangeDuringIndex(t *testing.T) {
	s := testService(t)
	var input strings.Builder
	for i := 0; i < 2001; i++ {
		input.WriteString("{\"name\":\"aaaa\"}\n")
	}
	path := filepath.Join(s.Root, "events.ndjson")
	os.WriteFile(path, []byte(input.String()), 0600)
	info, _ := os.Stat(path)
	checks := 0
	s.DiskCheck = func(string, uint64) error {
		checks++
		if checks == 2 {
			if e := os.WriteFile(path, []byte(strings.ReplaceAll(input.String(), "aaaa", "bbbb")), 0600); e != nil {
				return e
			}
			return os.Chtimes(path, info.ModTime(), info.ModTime())
		}
		return nil
	}
	if e := s.SyncFile(path); e != nil {
		t.Fatal(e)
	}
	c, e := s.byPath(path)
	if e != nil || c.Status != "failed" || !strings.Contains(c.Error, "contents changed") {
		t.Fatalf("checksum did not reject changed contents: %v %#v", e, c)
	}
}

func TestPagesDuringLongGenerationReader(t *testing.T) {
	s := testService(t)
	c, e := s.Import("data.json", strings.NewReader(`[{"id":1,"name":"old"}]`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	s.Sync()
	waitCollection(t, s, c.ID, "ready")
	c, _ = s.Get(c.ID)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- s.Each(context.Background(), c, func(any) error { close(started); <-release; return nil })
	}()
	<-started
	pageDone := make(chan error, 1)
	go func() { _, _, err := s.Page(context.Background(), c, 1, 20, false, ""); pageDone <- err }()
	select {
	case e := <-pageDone:
		if e != nil {
			close(release)
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("ordinary page blocked behind a long scan")
	}
	os.WriteFile(c.Path, []byte(`[{"id":1,"name":"new"}]`), 0600)
	if e = s.SyncFile(c.Path); e != nil {
		close(release)
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	rows, _, e := s.Page(context.Background(), c, 1, 20, false, "")
	if e != nil || rows[0].(map[string]any)["name"] != "new" {
		close(release)
		t.Fatal("new generation unavailable", e)
	}
	if _, e = os.Stat(filepath.Join(s.IndexDir, c.Generation)); e != nil {
		close(release)
		t.Fatal("active old generation deleted")
	}
	close(release)
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(s.IndexDir, c.Generation)); !os.IsNotExist(e) {
		t.Fatal("released generation retained", e)
	}
}

func waitCollection(t *testing.T, s *Service, id, status string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		c, e := s.Get(id)
		if e == nil && c.Status == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	c, _ := s.Get(id)
	t.Fatalf("wanted %s: %#v", status, c)
}
