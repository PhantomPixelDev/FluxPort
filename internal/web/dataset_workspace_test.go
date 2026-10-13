package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/jobs"
	"fluxport/internal/transformations"
	"github.com/xuri/excelize/v2"
)

func TestDatasetCleanupJourneyPreservesSource(t *testing.T) {
	app, s, _ := setup(t, "", "")
	source := "id,name\n1, Ada \n1, Ada \n2,Grace\n"
	col, err := s.Import("customers.csv", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	page, body := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	if !strings.Contains(body, "data-sheet-query") || !strings.Contains(body, "data-file-workspace") || strings.Contains(body, "Data explorer</h1>") {
		t.Fatal("dataset must open the unified explorer")
	}
	cookies, token := page.Cookies(), csrf(t, body)
	base := url.Values{"csrf": {token}, "name": {"Cleaned customers"}, "source": {"untrusted-source"}, "action": {"add"}, "cleanup": {"deduplicate"}, "field": {"id"}}
	resp, body := call(t, app, "POST", "/collections/"+col.ID+"/changes", base.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "Cleaned data") || !strings.Contains(body, "Remove duplicates by id") {
		t.Fatalf("cleanup preview: %d", resp.StatusCode)
	}
	dedup := collections.PipelineStep{Op: "deduplicate", Keys: []string{"id"}}
	b, _ := json.Marshal(dedup)
	base.Set("steps", string(b))
	base.Set("action", "undo")
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/changes", base.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 200 || strings.Contains(body, "<textarea name=\"steps\"") {
		t.Fatal("undo failed", resp.StatusCode)
	}
	trim := collections.PipelineStep{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}
	b2, _ := json.Marshal(trim)
	base["steps"] = []string{string(b), string(b2)}
	base.Set("action", "save")
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/changes", base.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal("save failed", resp.StatusCode)
	}
	saved, err := s.Pipelines()
	if err != nil || len(saved) != 1 || saved[0].Source != col.ID || len(saved[0].Steps) != 2 {
		t.Fatal("saved steps did not preserve the route's source", err)
	}
	if err = s.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	waitDownloadCollection(t, s, col.ID)
	base.Set("action", "run")
	base.Set("download_view", "compact")
	base.Set("revision", "1")
	base.Set("format", "xlsx")
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/changes?saved="+saved[0].ID, base.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 || !strings.Contains(resp.Header.Get("Location"), "tab=downloads") {
		t.Fatal("run must open Downloads", resp.StatusCode)
	}
	target, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	job := waitDatasetExport(t, s, target.Query().Get("download_job"))
	unchanged, err := s.GetPipeline(saved[0].ID)
	if err != nil || unchanged.Revision != 1 {
		t.Fatal("downloading rewrote saved cleanup", err)
	}
	if job.State != "ready" {
		t.Fatal(job.Error)
	}
	resp, bytes := call(t, app, "GET", "/jobs/"+job.ID+"/download", "", nil, cookies)
	book, err := excelize.OpenReader(strings.NewReader(bytes))
	if resp.StatusCode != 200 || err != nil {
		t.Fatal(resp.StatusCode, err)
	}
	defer book.Close()
	rows, err := book.GetRows(book.GetSheetList()[0])
	if err != nil || len(rows) != 3 || len(rows[1]) != 2 || rows[1][1] != "Ada" {
		t.Fatalf("unexpected cleaned Excel result: %#v %v", rows, err)
	}
	resp, original := call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, cookies)
	if resp.StatusCode != 200 || original != source {
		t.Fatal("cleanup changed the source")
	}
}

func waitDatasetExport(t *testing.T, s *collections.Service, id string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := s.Jobs.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "ready" || job.State == "failed" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup export timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDatasetCleanupIsolationAndValidation(t *testing.T) {
	app, s, col := setup(t, "", "")
	page, body := call(t, app, "GET", "/collections/"+col.ID+"/changes", "", nil, nil)
	form := url.Values{"csrf": {csrf(t, body)}, "action": {"add"}, "cleanup": {"rename"}, "field": {"name"}, "name": {"Rename people"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	resp, body := call(t, app, "POST", "/collections/"+col.ID+"/changes", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 422 || !strings.Contains(body, "enter its new name") {
		t.Fatal("missing rename target accepted", resp.StatusCode)
	}
	foreign, err := s.Import("other.json", strings.NewReader(`[{"name":"Other"}]`))
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := s.SavePipeline(collections.Pipeline{Name: "Foreign steps", Source: foreign.ID, Steps: []collections.PipelineStep{{Op: "deduplicate"}}})
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = call(t, app, "GET", "/collections/"+col.ID+"/changes?saved="+recipe.ID, "", nil, page.Cookies())
	if resp.StatusCode != 404 {
		t.Fatal("foreign steps exposed", resp.StatusCode)
	}
	form = url.Values{"csrf": {csrf(t, body)}, "download_view": {"compact"}, "pipeline": {recipe.ID}, "format": {"json"}}
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Saved steps not found for this dataset") {
		t.Fatal("foreign steps exported", resp.StatusCode)
	}
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/workbench/enable", url.Values{"csrf": {form.Get("csrf")}}.Encode(), headers, page.Cookies())
	if resp.StatusCode != 303 {
		t.Fatal("source editing enable failed", resp.StatusCode)
	}
	updated, err := s.Get(col.ID)
	if err != nil || !updated.CRUD || updated.Enabled != col.Enabled {
		t.Fatal("enabling editing changed unrelated API state", err)
	}
}
