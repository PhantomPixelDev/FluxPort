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
)

// Explicitly opt in: creates large sources and disposable indexes in TempDir.
// Reports measured Go heap, rather than claiming process RSS is the same thing.
func TestLargeStreamingInputs(t *testing.T) {
	if os.Getenv("FLUXPORT_LARGE_TESTS") != "1" {
		t.Skip("set FLUXPORT_LARGE_TESTS=1 for 100 MiB / 1 GiB verification")
	}
	results := []map[string]any{}
	for _, tc := range []struct {
		format string
		size   int64
	}{{"ndjson", 100 << 20}, {"csv", 100 << 20}, {"json", 100 << 20}, {"ndjson", 1 << 30}} {
		t.Run(fmt.Sprintf("%s-%d", tc.format, tc.size), func(t *testing.T) {
			s := testService(t)
			s.MaxBytes = 16 << 20
			path := filepath.Join(s.Root, "large."+tc.format)
			f, e := os.Create(path)
			if e != nil {
				t.Fatal(e)
			}
			w := bufio.NewWriterSize(f, 64<<10)
			padding := strings.Repeat("x", 1000)
			var size, count int64
			if tc.format == "json" {
				w.WriteByte('[')
				size++
			}
			if tc.format == "csv" {
				w.WriteString("id,payload\n")
				size += 11
			}
			for size < tc.size {
				var record string
				if tc.format == "csv" {
					record = fmt.Sprintf("%d,%s\n", count, padding)
				} else {
					record = fmt.Sprintf(`{"id":%d,"payload":"%s"}`, count, padding)
					if tc.format == "ndjson" {
						record += "\n"
					} else if count > 0 {
						record = "," + record
					}
				}
				w.WriteString(record)
				size += int64(len(record))
				count++
			}
			if tc.format == "json" {
				w.WriteByte(']')
				size++
			}
			if e = w.Flush(); e != nil {
				t.Fatal(e)
			}
			if e = f.Close(); e != nil {
				t.Fatal(e)
			}
			runtime.GC()
			var base runtime.MemStats
			runtime.ReadMemStats(&base)
			var peak atomic.Uint64
			peak.Store(base.HeapAlloc)
			done := make(chan struct{})
			finished := make(chan struct{})
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
			start := time.Now()
			e = s.SyncFile(path)
			elapsed := time.Since(start)
			close(done)
			<-finished
			if e != nil {
				t.Fatal(e)
			}
			c, e := s.byPath(path)
			if e != nil || c.Status != "ready" || c.Rows != count {
				t.Fatalf("%v %#v", e, c)
			}
			pageStart := time.Now()
			rows, total, e := s.Page(context.Background(), c, int((count-1)/20)+1, 20, false, "")
			latency := time.Since(pageStart)
			if e != nil || total != count || len(rows) == 0 {
				t.Fatal("pagination", total, e)
			}
			last := rows[len(rows)-1].(map[string]any)["id"]
			if fmt.Sprint(last) != fmt.Sprint(count-1) {
				t.Fatal("last ordinal", last, count-1)
			}
			if peak.Load()-base.HeapAlloc > 96<<20 {
				t.Fatalf("heap growth %d exceeds 96 MiB", peak.Load()-base.HeapAlloc)
			}
			result := map[string]any{"os": runtime.GOOS, "format": tc.format, "bytes": size, "records": count, "index_seconds": elapsed.Seconds(), "mib_per_second": float64(size) / (1 << 20) / elapsed.Seconds(), "page_ms": float64(latency.Microseconds()) / 1000, "baseline_heap_bytes": base.HeapAlloc, "peak_heap_bytes": peak.Load()}
			results = append(results, result)
			b, _ := json.Marshal(result)
			t.Log(string(b))
		})
	}
	if path := os.Getenv("FLUXPORT_BENCH_REPORT"); path != "" {
		b, _ := json.MarshalIndent(results, "", "  ")
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
