package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestPipelineEditorPreviewSaveExportAndAPI(t *testing.T) {
	app, s, col := setup(t, "", "")
	resp, body := call(t, app, "GET", "/pipelines?collection="+col.ID, "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "Data pipelines") {
		t.Fatal(resp.StatusCode, body)
	}
	token := csrf(t, body)
	cookies := resp.Cookies()
	form := url.Values{"csrf": {token}, "name": {"Active people"}, "source": {col.ID}, "steps": {`{"op":"filter","filters":[{"field":"active","op":"eq","value":true}]}`, `{"op":"rename","rules":{"rename":{"name":"display_name"}}}`}, "action": {"preview"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	resp, body = call(t, app, "POST", "/pipelines", form.Encode(), headers, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "Preview through step 2") || !strings.Contains(body, "display_name") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal(resp.StatusCode, body)
	}
	form.Set("action", "save")
	resp, body = call(t, app, "POST", "/pipelines", form.Encode(), headers, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	saved, err := s.Pipelines()
	if err != nil || len(saved) != 1 {
		t.Fatal(saved, err)
	}
	resp, body = call(t, app, "GET", "/pipelines/"+saved[0].ID+"/export", "", nil, cookies)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatal(resp.StatusCode, body)
	}
	var definition map[string]any
	if err = json.Unmarshal([]byte(body), &definition); err != nil {
		t.Fatal(err)
	}
	resp, body = call(t, app, "POST", "/api/pipelines/"+saved[0].ID+"/preview", `{"through":0}`, map[string]string{"Content-Type": "application/json"}, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "name") {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "POST", "/api/pipelines/"+saved[0].ID+"/run", `{"format":"ndjson"}`, map[string]string{"Content-Type": "application/json"}, nil)
	if resp.StatusCode != 202 {
		t.Fatal(resp.StatusCode, body)
	}
	if err = s.Update(col.ID, false, false, ""); err != nil {
		t.Fatal(err)
	}
	resp, _ = call(t, app, "POST", "/api/pipelines/"+saved[0].ID+"/run", `{}`, map[string]string{"Content-Type": "application/json"}, nil)
	if resp.StatusCode != 404 {
		t.Fatal("disabled source was exposed", resp.StatusCode)
	}
	form.Set("action", "preview")
	form.Set("steps", `{"op":"filter","filters":broken}`)
	resp, body = call(t, app, "POST", "/pipelines", form.Encode(), headers, cookies)
	if resp.StatusCode != 422 || !strings.Contains(body, "filters") || !strings.Contains(body, "broken") {
		t.Fatal("invalid step input was lost", resp.StatusCode, body)
	}
}
