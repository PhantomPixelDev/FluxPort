package web

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"
	"testing"
)

func TestCurrentCleanupDownloadUsesAllRowsWithoutSaving(t *testing.T) {
	app, service, _ := setup(t, "", "")
	var rows []string
	for i := 0; i < 60; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":%d,"name":" Ada ","big":9007199254740993}`, i/2))
	}
	source := "[" + strings.Join(rows, ",") + "]"
	col, err := service.Import("current.json", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	if err = service.Sync(); err != nil {
		t.Fatal(err)
	}
	waitDownloadCollection(t, service, col.ID)
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	draft := `{"version":1,"source":"untrusted","steps":[{"op":"deduplicate","keys":["id"]},{"op":"transform","transform":{"kind":"trim","field":"name"}}]}`
	form := url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "dataset": {"current"}, "draft": {draft}, "format": {"json"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	response, body := call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), headers, page.Cookies())
	if response.StatusCode != 303 {
		t.Fatal(response.StatusCode, body)
	}
	location, _ := url.Parse(response.Header.Get("Location"))
	job := waitDatasetExport(t, service, location.Query().Get("download_job"))
	if job.State != "ready" {
		t.Fatal(job.Error)
	}
	response, body = call(t, app, "GET", "/jobs/"+job.ID+"/download", "", nil, page.Cookies())
	_, disposition, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil || disposition["filename"] != "current-cleaned.json" {
		t.Fatalf("unexpected download name: %q (%v)", response.Header.Get("Content-Disposition"), err)
	}
	var output []map[string]any
	d := json.NewDecoder(strings.NewReader(body))
	d.UseNumber()
	if err = d.Decode(&output); err != nil || response.StatusCode != 200 || len(output) != 30 {
		t.Fatalf("full export: %d rows, %v", len(output), err)
	}
	for _, row := range output {
		if row["name"] != "Ada" || row["big"] != json.Number("9007199254740993") {
			t.Fatal("values changed", row)
		}
	}
	saved, _ := service.Pipelines()
	if len(saved) != 0 {
		t.Fatal("download created saved steps")
	}
	_, body = call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, page.Cookies())
	if body != source {
		t.Fatal("source changed")
	}
	_, body = call(t, app, "GET", location.String(), "", nil, page.Cookies())
	if !strings.Contains(body, "Changes preview") {
		t.Fatal("native download lost its data source")
	}
	form.Set("format", "original")
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), headers, page.Cookies())
	if !strings.Contains(body, "Choose an output format") {
		t.Fatal("current changes silently downloaded original")
	}
	form.Set("format", "json")
	form.Set("draft", `{"steps":[{"op":"not-supported"}]}`)
	_, body = call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), headers, page.Cookies())
	if !strings.Contains(body, "role=\"alert\"") {
		t.Fatal("invalid cleanup not actionable")
	}
	response, _ = call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), headers, nil)
	if response.StatusCode != 403 {
		t.Fatal("draft export lacks CSRF")
	}
}

func TestDownloadFilename(t *testing.T) {
	for _, tc := range []struct{ name, format, suffix, want string }{
		{"orders.csv", "xlsx", "", "orders.xlsx"},
		{"orders.csv", "json", "-cleaned", "orders-cleaned.json"},
		{"sales.JSON", "csv", "-filtered", "sales-filtered.csv"},
		{"山田 sales.xlsx", "parquet", "", "山田 sales.parquet"},
		{`C:\private\sales.csv`, "csv", "", "sales.csv"},
		{"../../sales.csv", "json", "", "sales.json"},
		{"bad\r\nname.csv", "xlsx", "", "badname.xlsx"},
		{"..", "json", "", "collection.json"},
	} {
		if got := downloadFilename(tc.name, tc.format, tc.suffix); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDraftDoesNotAffectOriginalDatasetChoice(t *testing.T) {
	app, _, col := setup(t, "", "")
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	form := url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "dataset": {"original"}, "draft": {"invalid"}, "format": {"original"}}
	resp, _ := call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 {
		t.Fatal("original download attempted to read cleanup draft", resp.StatusCode)
	}
}
