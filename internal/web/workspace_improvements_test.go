package web

import (
	"context"
	"fluxport/internal/collections"
	"fluxport/internal/transformations"
	"net/url"
	"strings"
	"testing"
)

func TestInsightsSuggestionsAndFormatReview(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("insights.json", strings.NewReader(`[{"id":9007199254740993,"name":" Ada ","nested":{"n":true},"empty":"","nil":null},{"id":9007199254740993,"name":" Ada ","nested":{"n":true},"empty":"","nil":null},{"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	page, body := call(t, app, "GET", "/collections/"+col.ID+"/insights?field=name", "", nil, nil)
	if page.StatusCode != 200 || !strings.Contains(body, "Preview trimming spaces") || !strings.Contains(body, "duplicate rows") || !strings.Contains(body, "Checked 3 of 3") {
		t.Fatal(page.StatusCode, body)
	}
	if !strings.Contains(body, `class="data-table insights-table"`) || !strings.Contains(body, `scope="col" class="insights-count">Missing`) || strings.Contains(body, `column-summary-grid`) {
		t.Fatal("Insights lack consistent column statistics table")
	}
	_, numericBody := call(t, app, "GET", "/collections/"+col.ID+"/insights?field=id", "", nil, nil)
	if !strings.Contains(numericBody, "9007199254740993") || !strings.Contains(numericBody, "<dt>Minimum</dt>") {
		t.Fatal("Numeric details lose exact integer statistics")
	}
	_, nativeBody := call(t, app, "GET", "/collections/"+col.ID+"/insights?detail=0", "", nil, nil)
	if !strings.Contains(nativeBody, `id="column-details-0" class="column-detail-row" >`) {
		t.Fatal("Native detail links do not expand the requested row")
	}
	form := url.Values{"csrf": {csrf(t, body)}, "format": {"xlsx"}}
	resp, body := call(t, app, "POST", "/collections/"+col.ID+"/export-check", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "numeric values become exact text cells") || !strings.Contains(body, "nested values become JSON text") || !strings.Contains(body, "Download file") {
		t.Fatal(resp.StatusCode, body)
	}
	form.Set("format", "toml")
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/export-check", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "cannot be converted") || strings.Contains(body, ">Download file<") {
		t.Fatal("incompatible TOML not blocked", resp.StatusCode)
	}
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/export-check", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, nil)
	if resp.StatusCode != 403 {
		t.Fatal("review lacks CSRF", resp.StatusCode)
	}
}

func TestVisualJoinSampleAndReuseKeepsOriginalRecipe(t *testing.T) {
	app, s, _ := setup(t, "", "")
	left, err := s.Import("left.json", strings.NewReader(`[{"key.x":1,"name":" Ada "},{"key.x":2,"name":"Grace"},{"name":"Missing"}]`))
	if err != nil {
		t.Fatal(err)
	}
	right, err := s.Import("right.json", strings.NewReader(`[{"key/y":1,"value":"a"},{"key/y":1,"value":"b"},{"key/y":3}]`))
	if err != nil {
		t.Fatal(err)
	}
	page, body := call(t, app, "GET", "/collections/"+left.ID+"/changes", "", nil, nil)
	form := url.Values{"csrf": {csrf(t, body)}, "name": {"Joined"}, "action": {"add_join"}, "join_left": {"key.x"}, "join_right": {"key/y"}, "join_collection": {right.ID}, "join_as": {"matched.data"}, "join_kind": {"left"}}
	resp, body := call(t, app, "POST", "/collections/"+left.ID+"/changes", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "1 matched, 2 unmatched; 1 have multiple matches") || !strings.Contains(body, "sample counts") {
		t.Fatal(resp.StatusCode, body)
	}
	form.Set("join_as", "name")
	resp, _ = call(t, app, "POST", "/collections/"+left.ID+"/changes", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 422 {
		t.Fatal("join overwrote a column")
	}
	recipe, err := s.SavePipeline(collections.Pipeline{Name: "Reusable cleanup", Source: left.ID, Steps: []collections.PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}})
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.Import("next-month.json", strings.NewReader(`[{"name":" Alan "}]`))
	if err != nil {
		t.Fatal(err)
	}
	resp, body = call(t, app, "GET", "/collections/"+target.ID+"/changes?reuse="+recipe.ID, "", nil, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Trim spaces in name") || !strings.Contains(body, "Steps copied for this dataset") {
		t.Fatal(resp.StatusCode, body)
	}
	original, err := s.GetPipeline(recipe.ID)
	if err != nil || original.Source != left.ID || original.Revision != recipe.Revision {
		t.Fatal("reuse changed original recipe")
	}
	original.Source = target.ID
	rows, err := s.PreviewPipeline(context.Background(), original, -1)
	if err != nil || rows[0].(map[string]any)["name"] != "Alan" {
		t.Fatal(rows, err)
	}
}
