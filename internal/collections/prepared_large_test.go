package collections

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fluxport/internal/transformations"
)

// Opt-in real source + result indexes. Heap is measured, not conflated with RSS.
func TestPreparedLargeWorkflow(t *testing.T) {
	if os.Getenv("FLUXPORT_PREPARED_LARGE_TESTS") != "1" {
		t.Skip("set FLUXPORT_PREPARED_LARGE_TESTS=1 for 100 MiB / 1 GiB cleaned-result verification")
	}
	report := []map[string]any{}
	for _, target := range []int64{100 << 20, 1 << 30} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			metrics := map[string]any{"version": "0.3.12", "os": runtime.GOOS, "go": runtime.Version(), "target_bytes": target, "passed": false}
			defer func() { report = append(report, metrics) }()
			s := testService(t)
			s.MaxBytes = 16 << 20
			path := filepath.Join(s.Root, "large.ndjson")
			f, e := os.Create(path)
			if e != nil {
				t.Fatal(e)
			}
			w := bufio.NewWriterSize(f, 64<<10)
			padding := strings.Repeat("x", 1000)
			var count, bytes int64
			for bytes < target {
				row := fmt.Sprintf("{\"id\":%d,\"name\":\" 東京 \",\"big\":9007199254740993,\"payload\":\"%s\"}\n", count, padding)
				if _, e = w.WriteString(row); e != nil {
					f.Close()
					t.Fatal(e)
				}
				bytes += int64(len(row))
				count++
			}
			if e = w.Flush(); e != nil {
				f.Close()
				t.Fatal(e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			runtime.GC()
			var baseline runtime.MemStats
			runtime.ReadMemStats(&baseline)
			var peak atomic.Uint64
			peak.Store(baseline.HeapAlloc)
			done, finished := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(finished)
				tick := time.NewTicker(10 * time.Millisecond)
				defer tick.Stop()
				for {
					select {
					case <-done:
						return
					case <-tick.C:
						var m runtime.MemStats
						runtime.ReadMemStats(&m)
						for old := peak.Load(); m.HeapAlloc > old && !peak.CompareAndSwap(old, m.HeapAlloc); old = peak.Load() {
						}
					}
				}
			}()
			defer func() {
				close(done)
				<-finished
				metrics["baseline_heap_bytes"] = baseline.HeapAlloc
				metrics["peak_heap_bytes"] = peak.Load()
			}()
			started := time.Now()
			if e = s.SyncFile(path); e != nil {
				t.Fatal(e)
			}
			metrics["index_seconds"] = time.Since(started).Seconds()
			c, e := s.byPath(path)
			if e != nil || c.Rows != count {
				t.Fatal(c, e)
			}
			if e = s.Jobs.Start(context.Background(), 1); e != nil {
				t.Fatal(e)
			}
			defer s.Jobs.Stop()
			cut := count / 2
			q := Query{Filters: []Filter{{Field: "id", Op: "gte", Value: cut}}, Sort: []Sort{{Field: "id", Desc: true}}, Page: 7, Limit: 20}
			p := Pipeline{Source: c.ID, InputQuery: &q, Steps: []PipelineStep{{Op: "select", Rules: transformations.Rules{Select: []string{"id", "name", "big"}}}, {Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}
			started = time.Now()
			j, e := s.PrepareResult(p)
			if e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(15 * time.Minute)
			for time.Now().Before(deadline) {
				j, e = s.Jobs.Get(j.ID)
				if e != nil {
					t.Fatal(e)
				}
				if j.State != "running" && j.State != "queued" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			metrics["prepare_seconds"] = time.Since(started).Seconds()
			if j.State != "ready" {
				t.Fatal(j.State, j.Error)
			}
			d, e := s.PreparedResult(c.ID, j.ID)
			if e != nil || d.Rows != count-cut {
				t.Fatal(d, e)
			}
			started = time.Now()
			result, e := s.QueryResult(context.Background(), c.ID, j.ID, Query{Page: int((d.Rows-1)/20) + 1, Limit: 20})
			metrics["page_ms"] = float64(time.Since(started).Microseconds()) / 1000
			if e != nil || result.Total != d.Rows || len(result.Records) == 0 {
				t.Fatal(result, e)
			}
			last := result.Records[len(result.Records)-1].Value.(map[string]any)
			if last["id"] != json.Number(fmt.Sprint(cut)) || last["name"] != "東京" || last["big"] != json.Number("9007199254740993") {
				t.Fatal("wrong final page", last)
			}
			if peak.Load()-baseline.HeapAlloc > 96<<20 {
				t.Fatal("heap growth exceeded 96 MiB", peak.Load()-baseline.HeapAlloc)
			}
			metrics["source_bytes"], metrics["source_rows"], metrics["cleaned_rows"], metrics["cleaned_payload_bytes"], metrics["passed"] = bytes, count, d.Rows, d.Bytes, true
		})
	}
	if path := os.Getenv("FLUXPORT_PREPARED_BENCH_REPORT"); path != "" {
		b, _ := json.MarshalIndent(report, "", "  ")
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := json.Marshal(report)
	t.Log(string(b))
}
