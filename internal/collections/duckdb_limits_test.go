package collections

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDuckDBQueuedQueryCancellation(t *testing.T) {
	s := testService(t)
	s.duckSlots = make(chan struct{}, 1)
	c, err := s.Import("bounded.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.acquireDuckDB(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.SQLWithEngine(ctx, []string{c.ID}, "SELECT COUNT(*) FROM c_"+c.ID, "duckdb")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queued query ignored cancellation", err)
	}
	release()
	if _, err = s.SQLWithEngine(context.Background(), []string{c.ID}, "SELECT COUNT(*) FROM c_"+c.ID, "duckdb"); err != nil {
		t.Fatal("slot leaked after cancellation", err)
	}
}
