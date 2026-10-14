package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"fluxport/internal/collections"
)

func TestScopedCleanedResultNativeWorkflowAndIsolation(t *testing.T) {
	app, s, _ := setup(t, "", "")
	var input strings.Builder
	for i := 0; i < 90; i++ {
		fmt.Fprintf(&input, "{\"id\":%d,\"group\":%d,\"name\":\" Ada \",\"a.b/x\":9007199254740993}\n", i, i%30)
	}
	col, e := s.Import("scoped.ndjson", strings.NewReader(input.String()))
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
	waitDownloadCollection(t, s, col.ID)
	col, e = s.Get(col.ID)
	if e != nil {
		t.Fatal(e)
	}
	base := "/collections/" + col.ID
	page, html := call(t, app, "GET", base, "", nil, nil)
	q := `{"filters":[{"field":"id","op":"gte","value":30}],"sort":[{"field":"id","desc":true}],"page":2,"limit":20}`
	definition := `{"version":1,"source":"untrusted","input_query":` + q + `,"steps":[{"op":"deduplicate","keys":["group"]},{"op":"rename","rules":{"rename":{"a.b/x":"new.name/x"}}}]}`
	form := url.Values{"csrf": {csrf(t, html)}, "definition": {definition}, "source_generation": {col.Generation}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	resp, body := call(t, app, "POST", base+"/cleaned-results", form.Encode(), headers, nil)
	if resp.StatusCode != 403 {
		t.Fatal("prepare lacked CSRF", resp.StatusCode)
	}
	resp, body = call(t, app, "POST", base+"/cleaned-results", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	location, _ := url.Parse(resp.Header.Get("Location"))
	resultID := location.Query().Get("result_id")
	prep := waitDatasetExport(t, s, resultID)
	if prep.State != "ready" {
		t.Fatal(prep)
	}
	resp, body = call(t, app, "GET", base+"?result_id="+resultID+"&query="+url.QueryEscape(`{"sort":[{"field":"id"}],"page":2,"limit":5}`), "", nil, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "Page 2 of 6") || !strings.Contains(body, "data-editable=\"false\"") || !strings.Contains(body, "data-cleaned-total=\"30\"") || !strings.Contains(body, "new.name/x") {
		t.Fatal(resp.StatusCode, body[:min(len(body), 1500)])
	}
	for _, match := range regexp.MustCompile(`href="([^"]+)" aria-label="(?:Page [0-9]+|Sort [^"]+)"`).FindAllStringSubmatch(body, -1) {
		if !strings.Contains(match[1], "result_id="+resultID) {
			t.Fatal("derived navigation changed dataset", match[1])
		}
	}
	resp, body = call(t, app, "GET", base+"?result_id="+resultID+"&tab=raw", "", nil, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "new.name/x") || !strings.Contains(body, "9007199254740993") {
		t.Fatal("raw read wrong dataset", resp.StatusCode)
	}
	resp, body = call(t, app, "GET", base+"/changes?result_id="+resultID, "", nil, page.Cookies())
	if resp.StatusCode != 200 || !strings.Contains(body, "name=\"input_query\"") || !strings.Contains(body, "new.name/x") {
		t.Fatal("cleanup lost transformed schema", resp.StatusCode)
	}
	form = url.Values{"csrf": {csrf(t, html)}, "download_view": {"compact"}, "dataset": {"current"}, "result_id": {resultID}, "query": {`{"filters":[{"field":"id","op":"gte","value":85}],"sort":[{"field":"id"}],"page":3,"limit":1}`}, "format": {"json"}}
	resp, body = call(t, app, "POST", base+"/export", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	location, _ = url.Parse(resp.Header.Get("Location"))
	exported := waitDatasetExport(t, s, location.Query().Get("download_job"))
	if exported.State != "ready" {
		t.Fatal(exported)
	}
	resp, body = call(t, app, "GET", "/jobs/"+exported.ID+"/download", "", nil, page.Cookies())
	var output []map[string]json.RawMessage
	if e = json.Unmarshal([]byte(body), &output); e != nil || len(output) != 5 || string(output[0]["id"]) != "85" {
		t.Fatal(e, body)
	}
	form.Set("format", "original")
	form.Set("original_shortcut", "1")
	resp, body = call(t, app, "POST", base+"/export", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 200 || body != input.String() {
		t.Fatal("original shortcut changed source")
	}
	other, e := s.Import("other.json", strings.NewReader(`[]`))
	if e != nil {
		t.Fatal(e)
	}
	resp, _ = call(t, app, "GET", "/collections/"+other.ID+"?result_id="+resultID, "", nil, page.Cookies())
	if resp.StatusCode != 404 {
		t.Fatal("cross-collection read", resp.StatusCode)
	}
	form.Set("format", "json")
	resp, body = call(t, app, "POST", "/collections/"+other.ID+"/export", form.Encode(), headers, page.Cookies())
	if strings.Contains(body, "download_job=") {
		t.Fatal("cross-collection export")
	}
	form.Set("method", "DELETE")
	form.Set("generation", col.Generation)
	resp, _ = call(t, app, "POST", base+"/workbench/edit", form.Encode(), headers, page.Cookies())
	if resp.StatusCode != 403 {
		t.Fatal("derived edit allowed", resp.StatusCode)
	}
	_, body = call(t, app, "GET", "/jobs?collection="+col.ID, "", nil, page.Cookies())
	if strings.Contains(body, "other.json") || !strings.Contains(body, "prepare_result") {
		t.Fatal("history not collection-specific")
	}
	_, count, e := s.List("", 1, 100)
	if e != nil || count != 3 {
		t.Fatal("result registered as collection", count, e)
	}
	stored, _, e := s.PreparedDefinition(col.ID, resultID)
	if e != nil || stored.InputQuery.Page != 0 || stored.InputQuery.Limit != 0 {
		t.Fatal(stored, e)
	}
	got, e := s.QueryResult(context.Background(), col.ID, resultID, collections.Query{Limit: 100})
	if e != nil || got.Total != 30 {
		t.Fatal(got, e)
	}
}
