package web

import (
	"encoding/json"
	"fluxport/internal/collections"
	"net/url"
	"strings"
	"testing"
)

func TestFileCommandFormsAndCSRF(t *testing.T) {
	app, s, col := setup(t, "", "")
	response, body := call(t, app, "GET", "/collections/"+col.ID+"?edit=add_row", "", nil, nil)
	cookies := response.Cookies()
	token := csrf(t, body)
	col, _ = s.Get(col.ID)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	cmd := collections.FileCommand{Generation: col.Generation, Action: "add_row", Value: map[string]any{"name": "New row"}}
	encoded, _ := json.Marshal(cmd)
	form := url.Values{"command": {string(encoded)}}
	response, _ = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode != 403 {
		t.Fatal("CSRF bypass", response.StatusCode)
	}
	form.Set("csrf", token)
	form.Set("preview", "1")
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, `"rows":1`) {
		t.Fatal(response.StatusCode, body)
	}
	current, _ := s.Get(col.ID)
	if current.Generation != col.Generation {
		t.Fatal("preview wrote file")
	}
	form.Del("preview")
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, `"saved":true`) {
		t.Fatal(response.StatusCode, body)
	}
	response, _ = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), headers, cookies)
	if response.StatusCode != 409 {
		t.Fatal("stale command allowed", response.StatusCode)
	}
	// Native forms receive the same wide preview, not JSON.
	current, _ = s.Get(col.ID)
	form = url.Values{"csrf": {token}, "command_action": {"add_column"}, "column_name": {"new/key.dot"}, "generation": {current.Generation}, "preview": {"1"}}
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "Sample only") || !strings.Contains(body, "new/key.dot") {
		t.Fatal(response.StatusCode, body)
	}
}
func TestEditableCopyEndpointOwnership(t *testing.T) {
	app, s, col := setup(t, "", "")
	response, body := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	cookies := response.Cookies()
	col, _ = s.Get(col.ID)
	form := url.Values{"csrf": {csrf(t, body)}, "generation": {col.Generation}}
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/editable-copy", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, cookies)
	if response.StatusCode != 202 {
		t.Fatal(response.StatusCode, body)
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &job); err != nil {
		t.Fatal(err)
	}
	response, _ = call(t, app, "GET", "/collections/"+strings.Repeat("a", 24)+"/editable-copy/"+job.ID, "", nil, cookies)
	if response.StatusCode != 404 {
		t.Fatal("cross-owner status accepted")
	}
	response, body = call(t, app, "GET", "/collections/"+col.ID+"/editable-copy/"+job.ID, "", nil, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, `"state":"queued"`) {
		t.Fatal(response.StatusCode, body)
	}
}
