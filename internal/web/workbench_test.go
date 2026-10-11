package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"fluxport/internal/collections"
)

func TestWorkbenchViewsEditsAndSQLRoutes(t *testing.T) {
	app, s, col := setup(t, "", "")
	resp, body := call(t, app, "GET", "/collections/"+col.ID+"/workbench", "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "Dataset workspace") || !strings.Contains(body,"data-sheet-query") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal(resp.StatusCode, body)
	}
	cookies := resp.Cookies()
	token := csrf(t, body)
	form := url.Values{"csrf": {token}, "name": {"Active people"}, "query": {`{"filters":[{"field":"active","op":"eq","value":true}]}`}, "columns": {"name"}}
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/views", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "GET", resp.Header.Get("Location"), "", nil, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "1 matching records") {
		t.Fatal(resp.StatusCode, body)
	}
	if err := s.Update(col.ID, true, true, ""); err != nil {
		t.Fatal(err)
	}
	result, err := s.Query(context.Background(), col, collections.Query{})
	if err != nil {
		t.Fatal(err)
	}
	form = url.Values{"csrf": {token}, "generation": {result.Generation}, "method": {"PATCH"}, "record_id": {result.Records[0].ID}, "value": {`{"name":"Updated"}`}}
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	resp, _ = call(t, app, "POST", "/collections/"+col.ID+"/workbench/edit", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 409 {
		t.Fatal("stale edit not rejected", resp.StatusCode)
	}
	resp, body = call(t, app, "GET", "/sql", "", nil, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "SQL playground") || !strings.Contains(body, `name="engine"`) || !strings.Contains(body, "Native SQLite") {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = call(t, app, "GET", "/sql?collection="+col.ID, "", nil, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, `SELECT * FROM &#34;c_`+col.ID+`&#34; LIMIT 100`) || !strings.Contains(body, `value="`+col.ID+`" checked`) {
		t.Fatal("collection SQL shortcut did not select its dataset", resp.StatusCode, body)
	}
	form = url.Values{"csrf": {token}, "collections": {col.ID}, "sql": {"SELECT name FROM \"c_" + col.ID + "\" ORDER BY name"}, "action": {"run"}, "engine": {"native"}}
	resp, body = call(t, app, "POST", "/sql", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "Updated") || !strings.Contains(body, "SQLite ·") {
		t.Fatal(resp.StatusCode, body)
	}
	form.Set("action", "save")
	form.Set("name", "People names")
	form.Set("published", "on")
	resp, body = call(t, app, "POST", "/sql", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode, body)
	}
	queries, err := s.SavedSQL()
	if err != nil || len(queries) != 1 {
		t.Fatal(queries, err)
	}
	resp, body = call(t, app, "GET", "/api/queries/"+queries[0].ID, "", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, body)
	}
	var decoded map[string]any
	if err = json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
}
