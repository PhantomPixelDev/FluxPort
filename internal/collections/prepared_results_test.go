package collections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/transformations"
)

func waitResultJob(t *testing.T, s *Service, id string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		j, e := s.Jobs.Get(id)
		if e != nil {
			t.Fatal(e)
		}
		if j.State != "running" && j.State != "queued" && j.State != "staged" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not complete", id)
	return jobs.Job{}
}

func startResultService(t *testing.T, s *Service, c Collection) Collection {
	t.Helper()
	if e := s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Stop)
	if e := s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	c, e := s.Get(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestPreparedResultCapturesAllPagesSortAndSeparateOutputQuery(t *testing.T) {
	s := testService(t)
	var input strings.Builder
	for i := 0; i < 120; i++ {
		fmt.Fprintf(&input, "{\"id\":%d,\"group\":%d,\"name\":\" 東京 \",\"a.b/x\":9007199254740993,\"nested\":{\"v\":[true,null]},\"empty\":\"\",\"null\":null}\n", i, i%12)
	}
	c, e := s.Import("rows.ndjson", strings.NewReader(input.String()))
	if e != nil {
		t.Fatal(e)
	}
	c = startResultService(t, s, c)
	before, e := os.ReadFile(c.Path)
	if e != nil {
		t.Fatal(e)
	}
	q := Query{Filters: []Filter{{Field: "id", Op: "gte", Value: json.Number("40")}}, Sort: []Sort{{Field: "id", Desc: true}}, Page: 3, Limit: 20}
	p := Pipeline{Source: c.ID, InputQuery: &q, Steps: []PipelineStep{{Op: "deduplicate", Keys: []string{"group"}}, {Op: "rename", Rules: transformations.Rules{Rename: map[string]string{"a.b/x": "new.name/x"}}}}}
	j, e := s.PrepareResult(p)
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	d, e := s.PreparedResult(c.ID, j.ID)
	if e != nil || d.Rows != 12 || d.InputQuery.Page != 0 || d.InputQuery.Limit != 0 {
		t.Fatal(d, e)
	}
	got, e := s.QueryResult(context.Background(), c.ID, j.ID, Query{Page: 2, Limit: 5})
	if e != nil || got.Total != 12 || len(got.Records) != 5 {
		t.Fatal(got, e)
	}
	row := got.Records[0].Value.(map[string]any)
	if row["id"] != json.Number("114") || row["new.name/x"] != json.Number("9007199254740993") || row["empty"] != "" || row["null"] != nil {
		t.Fatal(row)
	}
	if _, ok := row["missing"]; ok {
		t.Fatal("missing changed")
	}
	outQ := Query{Filters: []Filter{{Field: "id", Op: "gte", Value: json.Number("115")}}, Sort: []Sort{{Field: "id"}}, Page: 2, Limit: 2}
	filtered, e := s.QueryResult(context.Background(), c.ID, j.ID, outQ)
	if e != nil || filtered.Total != 5 {
		t.Fatal(filtered, e)
	}
	ex, e := s.ExportResultJob(c.ID, j.ID, "json", outQ, conversion.Options{})
	if e != nil {
		t.Fatal(e)
	}
	ex = waitResultJob(t, s, ex.ID)
	if ex.State != "ready" || ex.Records != 5 {
		t.Fatal(ex)
	}
	b, e := os.ReadFile(ex.Result)
	if e != nil {
		t.Fatal(e)
	}
	v, e := conversion.Decode(b, "json")
	if e != nil {
		t.Fatal(e)
	}
	rows := v.([]any)
	if len(rows) != 5 || rows[0].(map[string]any)["id"] != json.Number("115") {
		t.Fatal(rows)
	}
	p.Name = "another label"
	p.InputQuery.Page = 99
	reused, e := s.PrepareResult(p)
	if e != nil || reused.ID != j.ID {
		t.Fatal("identical preparation not reused", reused, e)
	}
	p.Name = "Scoped recipe"
	saved, e := s.SavePipeline(p)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := s.GetPipeline(saved.ID)
	if e != nil || restored.InputQuery == nil || restored.InputQuery.Page != 0 || restored.InputQuery.Filters[0].Value != json.Number("40") {
		t.Fatal(restored, e)
	}
	after, e := os.ReadFile(c.Path)
	if e != nil || string(after) != string(before) {
		t.Fatal("original changed", e)
	}
	_, count, e := s.List("", 1, 100)
	if e != nil || count != 1 || filepath.Dir(ex.Result) == s.Root {
		t.Fatal("result became collection", count, e)
	}
	other, e := s.Import("other.json", strings.NewReader("[]"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.QueryResult(context.Background(), other.ID, j.ID, Query{}); e == nil {
		t.Fatal("cross-collection result read")
	}
}

func TestPreparedResultDependencyDoesNotOccupyWorkerAndCancellationRetry(t *testing.T) {
	s := testService(t)
	c, e := s.Import("rows.json", strings.NewReader(`[{"v":1},{"v":2}]`))
	if e != nil {
		t.Fatal(e)
	}
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	s.Jobs.Register("prepare_result", func(ctx context.Context, j jobs.Job, p func(int64, int64)) (string, error) {
		entered <- struct{}{}
		select {
		case <-gate:
			return s.prepareResultJob(ctx, j, p)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	c = startResultService(t, s, c)
	prep, e := s.PrepareResult(Pipeline{Source: c.ID})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ex, e := s.ExportResultJob(c.ID, prep.ID, "json", Query{}, conversion.Options{})
	if e != nil || ex.State != "staged" {
		t.Fatal(ex, e)
	}
	if e = s.Jobs.Cancel(prep.ID); e != nil {
		t.Fatal(e)
	}
	ex = waitResultJob(t, s, ex.ID)
	if ex.State != "failed" || !strings.Contains(ex.Error, "Retry preparation") {
		t.Fatal(ex)
	}
	close(gate)
	retry, e := s.RetryJob(ex.ID)
	if e != nil {
		t.Fatal(e)
	}
	retry = waitResultJob(t, s, retry.ID)
	if retry.State != "ready" || retry.Records != 2 {
		t.Fatal(retry)
	}
	// A staged export does not hold the only worker hostage. A later ordinary job completes.
	waiting, e := s.Jobs.SubmitWaiting("convert", c.ID, "waiting", `{"depends_on":"never-created"}`, 0)
	if e != nil {
		t.Fatal(e)
	}
	waiting = waitResultJob(t, s, waiting.ID)
	if waiting.State != "failed" {
		t.Fatal(waiting)
	}
}

func TestPreparedResultExpiryLeasesLowDiskAndSourceChange(t *testing.T) {
	s := testService(t)
	c, e := s.Import("rows.json", strings.NewReader(`[{"v":1}]`))
	if e != nil {
		t.Fatal(e)
	}
	c = startResultService(t, s, c)
	p := Pipeline{Source: c.ID}
	j, e := s.PrepareResult(p)
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	_, ref, e := s.resultReference(c.ID, j.ID)
	if e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-25 * time.Hour)
	os.Chtimes(ref.path, old, old)
	os.Chtimes(j.Result, old, old)
	s.cleanupResults()
	if _, e = os.Stat(ref.path); e != nil {
		t.Fatal("leased result deleted")
	}
	ref.release()
	s.cleanupResults()
	if _, e = os.Stat(ref.path); !os.IsNotExist(e) {
		t.Fatal("expired result retained", e)
	}
	if _, e = s.PreparedResult(c.ID, j.ID); e == nil {
		t.Fatal("expired result readable")
	}
	j, e = s.PrepareResult(p)
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if e = os.WriteFile(c.Path, []byte(`[{"v":3}]`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.QueryResult(context.Background(), c.ID, j.ID, Query{}); e == nil {
		t.Fatal("stale result served")
	}
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	s.DiskCheck = func(string, uint64) error { return errors.New("reserved disk space unavailable") }
	j, e = s.PrepareResult(p)
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if j.State != "failed" || !strings.Contains(j.Error, "disk space") {
		t.Fatal(j)
	}
	files, _ := os.ReadDir(s.ResultDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), j.ID) {
			t.Fatal("failed result left partial", f.Name())
		}
	}
}

func TestPreparedResultsRestartRecoveryAndAllFormats(t *testing.T) {
	s := testService(t)
	c, e := s.Import("formats.json", strings.NewReader(`[{"id":9007199254740993,"text":"東京","nested":{"v":true}},{"id":2,"text":"hello","nested":{"v":false}}]`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Stop)
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	c, _ = s.Get(c.ID)
	j, e := s.PrepareResult(Pipeline{Source: c.ID})
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	for _, format := range DatasetFormats() {
		ex, e := s.ExportResultJob(c.ID, j.ID, format, Query{}, conversion.Options{})
		if e != nil {
			t.Fatal(format, e)
		}
		ex = waitResultJob(t, s, ex.ID)
		if ex.State != "ready" || ex.Records != 2 {
			t.Fatal(format, ex)
		}
		if info, e := os.Stat(ex.Result); e != nil || info.Size() == 0 {
			t.Fatal(format, e)
		}
		if conversion.Supported(format) {
			b, _ := os.ReadFile(ex.Result)
			if _, e = conversion.Decode(b, format); e != nil {
				t.Fatal(format, e)
			}
		}
	}
	s.Stop()
	queued, e := s.PrepareResult(Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "limit", Limit: 1}}})
	if e != nil {
		t.Fatal(e)
	}
	partial := filepath.Join(s.ResultDir, queued.ID+".db.tmp")
	if e = os.WriteFile(partial, []byte("partial"), 0600); e != nil {
		t.Fatal(e)
	}
	s2, e := New(s.DB, s.Root, s.MaxBytes)
	if e != nil {
		t.Fatal(e)
	}
	if e = s2.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer s2.Stop()
	if e = s2.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s2, c.ID, "ready")
	interrupted, _ := s2.Jobs.Get(queued.ID)
	if interrupted.State != "interrupted" {
		t.Fatal(interrupted)
	}
	if _, e = os.Stat(partial); !os.IsNotExist(e) {
		t.Fatal("abandoned partial survived restart", e)
	}
	result, e := s2.QueryResult(context.Background(), c.ID, j.ID, Query{})
	if e != nil || result.Total != 2 {
		t.Fatal("ready descriptor lost after restart", result, e)
	}
	retry, e := s2.RetryJob(queued.ID)
	if e != nil {
		t.Fatal(e)
	}
	retry = waitResultJob(t, s2, retry.ID)
	if retry.State != "ready" || retry.Records != 1 {
		t.Fatal(retry)
	}
}

func TestPreparedResultRejectsMidScanChangesAndInvalidOutput(t *testing.T) {
	s := testService(t)
	var input strings.Builder
	for i := 0; i < 2400; i++ {
		fmt.Fprintf(&input, "{\"id\":%d,\"null\":null}\n", i)
	}
	c, e := s.Import("changing.ndjson", strings.NewReader(input.String()))
	if e != nil {
		t.Fatal(e)
	}
	c = startResultService(t, s, c)
	j, e := s.PrepareResult(Pipeline{Source: c.ID})
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	ex, e := s.ExportResultJob(c.ID, j.ID, "toml", Query{}, conversion.Options{})
	if e != nil {
		t.Fatal(e)
	}
	ex = waitResultJob(t, s, ex.ID)
	if ex.State != "failed" || ex.Result != "" {
		t.Fatal("incompatible output published", ex)
	}
	originalCheck := s.DiskCheck
	changed := false
	checks := 0
	s.DiskCheck = func(path string, reserve uint64) error {
		if path == s.ResultDir {
			checks++
		}
		if path == s.ResultDir && checks == 2 && !changed {
			changed = true
			if e := os.WriteFile(c.Path, []byte("{\"id\":99}\n"), 0600); e != nil {
				return e
			}
		}
		return originalCheck(path, reserve)
	}
	j, e = s.PrepareResult(Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "sort", Sort: []Sort{{Field: "id", Desc: true}}}}})
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if (j.State != "failed" && j.State != "cancelled") || j.Result != "" {
		t.Fatal("changed source published", j)
	}
	if _, e = os.Stat(filepath.Join(s.ResultDir, j.ID+".json")); !os.IsNotExist(e) {
		t.Fatal("stale manifest published", e)
	}
}

func TestPreparedEmptyResultRetainsRenamedCSVColumns(t *testing.T) {
	s := testService(t)
	c, e := s.Import("empty-scope.json", strings.NewReader(`[{"id":1,"a.b/x":"東京"}]`))
	if e != nil {
		t.Fatal(e)
	}
	c = startResultService(t, s, c)
	q := Query{Filters: []Filter{{Field: "id", Op: "gt", Value: 99}}}
	j, e := s.PrepareResult(Pipeline{Source: c.ID, InputQuery: &q, Steps: []PipelineStep{{Op: "rename", Rules: transformations.Rules{Rename: map[string]string{"a.b/x": "renamed/x"}}}}})
	if e != nil {
		t.Fatal(e)
	}
	j = waitResultJob(t, s, j.ID)
	if j.State != "ready" {
		t.Fatal(j)
	}
	d, e := s.PreparedResult(c.ID, j.ID)
	if e != nil || d.Rows != 0 || len(d.Fields) != 2 {
		t.Fatal(d, e)
	}
	ex, e := s.ExportResultJob(c.ID, j.ID, "csv", Query{}, conversion.Options{})
	if e != nil {
		t.Fatal(e)
	}
	ex = waitResultJob(t, s, ex.ID)
	if ex.State != "ready" {
		t.Fatal(ex)
	}
	b, e := os.ReadFile(ex.Result)
	if e != nil || string(b) != "id,renamed/x\n" {
		t.Fatal(string(b), e)
	}
}
