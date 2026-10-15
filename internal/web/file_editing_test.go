package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"fluxport/internal/collections"
)

func TestFileEditorHistoryAndCompatibility(t *testing.T) {
	app, s, col := setup(t, "", "")
	if err := s.Update(col.ID, false, false, ""); err != nil {
		t.Fatal(err)
	}
	col, _ = s.Get(col.ID)
	page, body := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	if strings.Contains(body, "workspace-original") || strings.Contains(body, "data-workspace-view=") {
		t.Fatal("competing file versions remain")
	}
	cookies, token := page.Cookies(), csrf(t, body)
	_, before := call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, cookies)
	rows, err := s.Query(context.Background(), col, collections.Query{})
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {token}, "generation": {rows.Generation}, "method": {"FIELD"}, "record_id": {rows.Records[0].ID}, "value": {`{"field":"/name","value":"Edited 山田"}`}, "file_mode": {"1"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	response, body := call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, nil)
	if response.StatusCode != 403 {
		t.Fatal("missing CSRF cookie was accepted")
	}
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, `"saved":true`) {
		t.Fatal(response.StatusCode, body)
	}
	col, _ = s.Get(col.ID)
	if col.Enabled || col.CRUD {
		t.Fatal("file editor enabled public mutations")
	}
	form.Del("file_mode")
	form.Set("generation", col.Generation)
	response, _ = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode == 200 || response.StatusCode == 303 {
		t.Fatal("legacy edit unexpectedly bypassed CRUD flags")
	}
	h, err := s.FileHistory(col.ID)
	if err != nil {
		t.Fatal(err)
	}
	restore := url.Values{"csrf": {token}, "generation": {col.Generation}, "current_revision": {strconv.FormatInt(h.Current, 10)}, "action": {"undo"}}
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/file-history", restore.Encode(), headers, cookies)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode, body)
	}
	_, after := call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, cookies)
	if after != before {
		t.Fatal("Undo did not restore exact source bytes")
	}
	response, _ = call(t, app, "POST", "/collections/"+col.ID+"/file-history", restore.Encode(), headers, cookies)
	if response.StatusCode != 409 {
		t.Fatal("stale history action accepted")
	}
	response, body = call(t, app, "GET", "/collections/"+col.ID+"/file-history", "", nil, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "file-history-table") {
		t.Fatal("native history missing")
	}
}

func TestFileCleanupNativeSaveAndUndo(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("trim.csv", strings.NewReader("id,name\r\n1, Ada \r\n2, Grace \r\n"))
	if err != nil {
		t.Fatal(err)
	}
	page, body := call(t, app, "GET", "/collections/"+col.ID+"/changes", "", nil, nil)
	token, cookies := csrf(t, body), page.Cookies()
	if err = s.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	waitDownloadCollection(t, s, col.ID)
	col, _ = s.Get(col.ID)
	form := url.Values{"csrf": {token}, "file_mode": {"1"}, "source_generation": {col.Generation}, "action": {"add"}, "cleanup": {"trim"}, "field": {"name"}, "scope": {"entire"}, "validation": {"[]"}}
	response, body := call(t, app, "POST", "/collections/"+col.ID+"/changes", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 303 {
		t.Fatal(response.StatusCode, body)
	}
	target, _ := url.Parse(response.Header.Get("Location"))
	job := waitDatasetExport(t, s, target.Query().Get("file_job"))
	if job.Kind != "file_cleanup" {
		t.Fatal(job.Kind)
	}
	status, jobBody := call(t, app, "GET", "/collections/"+col.ID+"/file-history?format=json&job_id="+job.ID, "", nil, cookies)
	if status.StatusCode != 200 || !strings.Contains(jobBody, "\"job_state\":\"ready\"") {
		t.Fatal("missed SSE recovery failed", status.StatusCode, jobBody)
	}
	status, _ = call(t, app, "GET", "/collections/"+col.ID+"/file-history?format=json&job_id=missing", "", nil, cookies)
	if status.StatusCode != 404 {
		t.Fatal("invalid job accepted")
	}
	waitDownloadCollection(t, s, col.ID)
	_, body = call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, cookies)
	if strings.Contains(body, " Ada ") || strings.Contains(body, " Grace ") {
		t.Fatal("cleanup did not save file")
	}
	_, body = call(t, app, "GET", "/collections/"+col.ID+"/file-history?format=json", "", nil, cookies)
	var state struct {
		History    collections.FileHistory `json:"history"`
		Generation string                  `json:"generation"`
	}
	if err = json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatal(err)
	}
	if err = s.RestoreFile(col.ID, state.Generation, "undo", 0, state.History.Current); err != nil {
		t.Fatal(err)
	}
	_, body = call(t, app, "GET", "/collections/"+col.ID+"/export?original=1", "", nil, cookies)
	if body != "id,name\r\n1, Ada \r\n2, Grace \r\n" {
		t.Fatal("native cleanup Undo lost exact bytes")
	}
}
