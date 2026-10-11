package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestQualityUIAndAPI(t *testing.T) {
	app, _, col := setup(t, "", "")
	path := "/collections/" + col.ID + "/quality"
	response, body := call(t, app, "GET", path, "", nil, nil)
	if response.StatusCode != 200 || !strings.Contains(body, "Scan coverage") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal(response.StatusCode, body)
	}
	form := url.Values{"csrf": {csrf(t, body)}, "limit": {"1"}, "rules": {`[{"field":"email","required":true}]`}, "action": {"save"}}
	response, body = call(t, app, "POST", path, form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, response.Cookies())
	if response.StatusCode != 200 || !strings.Contains(body, "Partial scan") || !strings.Contains(body, "Validation rules saved") {
		t.Fatal(response.StatusCode, body)
	}
	response, body = call(t, app, "POST", "/api/collections/"+col.ID+"/validate", `{}`, map[string]string{"Content-Type": "application/json"}, nil)
	var report struct {
		Validation struct {
			Errors int `json:"error_count"`
		}
	}
	if err := json.Unmarshal([]byte(body), &report); err != nil || response.StatusCode != 200 || report.Validation.Errors != 2 {
		t.Fatal(response.StatusCode, body, err)
	}
	protected, _, c := setup(t, "", "secret")
	response, _ = call(t, protected, http.MethodGet, "/api/collections/"+c.ID+"/profile", "", nil, nil)
	if response.StatusCode != 401 {
		t.Fatal("profile bypassed auth", response.StatusCode)
	}
}
