package web

import (
	"net/url"
	"strings"
	"testing"

	"fluxport/internal/collections"
	"fluxport/internal/transformations"
)

// Task 4: the download form keeps one stable shape — labeled source, rows,
// format; "Check format changes" sits directly in the form; saved steps show
// under their steps name; a queued job says it will download automatically.
func TestDownloadChooserStaysPredictable(t *testing.T) {
	app, service, col := setup(t, "", "")
	visit, html := call(t, app, "GET", "/collections/"+col.ID+"?tab=downloads", "", nil, nil)
	if visit.StatusCode != 200 {
		t.Fatal(visit.StatusCode)
	}
	if !strings.Contains(html, `<label class="field-label">File data<select name="dataset"`) {
		t.Fatal("source select lost its plain label")
	}
	if !strings.Contains(html, `<option value="original">Current file (as stored)</option>`) {
		t.Fatal("original option wording changed")
	}
	if !strings.Contains(html, `<option value="matching">Filtered rows</option>`) || !strings.Contains(html, `<option value="entire">All rows</option>`) {
		t.Fatal("rows select wording changed or values broken")
	}
	review := strings.Index(html, "Check format changes")
	details := strings.Index(html, `<details class="download-notes"`)
	if review < 0 || details < 0 || review > details {
		t.Fatal("Check format changes is not directly in the form")
	}
	if _, err := service.SavePipeline(collections.Pipeline{Name: "Trim names", Source: col.ID, Steps: []collections.PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}); err != nil {
		t.Fatal(err)
	}
	_, html = call(t, app, "GET", "/collections/"+col.ID+"?tab=downloads", "", nil, visit.Cookies())
	if !strings.Contains(html, "Saved steps: Trim names") || strings.Contains(html, "Saved cleanup:") {
		t.Fatal("saved steps option uses the steps vocabulary")
	}
	form := url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "dataset": {"original"}, "format": {"json"}, "scope": {"entire"}}
	response, body := call(t, app, "POST", "/collections/"+col.ID+"/export", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, visit.Cookies())
	if response.StatusCode != 303 {
		t.Fatal(response.StatusCode, body)
	}
	status, statBody := call(t, app, "GET", response.Header.Get("Location"), "", nil, visit.Cookies())
	if status.StatusCode != 200 || !strings.Contains(statBody, "Creating your file…") {
		t.Fatalf("queued status not shown (%d)", status.StatusCode)
	}
	if !strings.Contains(statBody, "Your download will start automatically when the file is ready.") {
		t.Fatal("queued job does not explain the automatic download")
	}
}
