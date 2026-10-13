package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestCollectionSQLKeepsCurrentSourceAndCSRF(t *testing.T) {
	app, s, col := setup(t, "", "")
	other, err := s.Import("unrelated.json", strings.NewReader(`[{"secret":"other"}]`))
	if err != nil {
		t.Fatal(err)
	}
	route := "/collections/" + col.ID + "/sql"
	page, body := call(t, app, "GET", route, "", nil, nil)
	if page.StatusCode != 200 || !strings.Contains(body, `c_`+col.ID) || strings.Contains(body, "Choose up to 8 datasets") || strings.Contains(body, other.Name) {
		t.Fatal("SQL must open on the current file")
	}
	form := url.Values{"csrf": {csrf(t, body)}, "action": {"run"}, "sql": {`SELECT COUNT(*) AS total FROM "c_` + col.ID + `"`}, "collections": {other.ID}, "engine": {"native"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	response, body := call(t, app, "POST", route, form.Encode(), headers, page.Cookies())
	if response.StatusCode != 200 || !strings.Contains(body, "<h2>Results</h2>") {
		t.Fatal("Scoped SQL lost its selected source", response.StatusCode)
	}
	form.Set("sql", `SELECT * FROM "c_`+other.ID+`"`)
	response, _ = call(t, app, "POST", route, form.Encode(), headers, page.Cookies())
	if response.StatusCode != 422 {
		t.Fatal("Scoped SQL included unrelated posted source", response.StatusCode)
	}
	response, _ = call(t, app, "POST", route, form.Encode(), headers, nil)
	if response.StatusCode != 403 {
		t.Fatal("SQL route lacks CSRF protection", response.StatusCode)
	}
}
