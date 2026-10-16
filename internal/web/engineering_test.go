package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/url"
	"strings"
	"testing"
)

func TestEngineeringFormsContractsComparisonAndSQL(t *testing.T) {
	app, s, col := setup(t, "", "")
	base := "/collections/" + col.ID
	response, body := call(t, app, "GET", base+"/quality", "", nil, nil)
	cookies := response.Cookies()
	token := csrf(t, body)
	for _, label := range []string{"Compare with another file", "Data contract", "Download checked records"} {
		if !strings.Contains(body, label) {
			t.Fatal("missing control", label)
		}
	}
	form := url.Values{"csrf": {token}, "action": {"capture_contract"}}
	response, body = call(t, app, "POST", base+"/quality", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "Contract captured") {
		t.Fatal(response.StatusCode, body)
	}
	contract, err := s.Contract(col.ID)
	if err != nil || contract == nil || len(contract.Columns) != 2 {
		t.Fatal(contract, err)
	}
	form = url.Values{"csrf": {token}, "action": {"compare"}, "other": {col.ID}, "keys": {`["name"]`}}
	response, body = call(t, app, "POST", base+"/quality", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "X-Fluxport-View": "workspace"}, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "2 unchanged") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal(response.StatusCode, body)
	}
	form = url.Values{"csrf": {token}, "action": {"run"}, "sql": {"SELECT :value AS result"}, "parameters": {`{"value":"東京"}`}, "engine": {"native"}}
	response, body = call(t, app, "POST", base+"/sql", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "東京") {
		t.Fatal(response.StatusCode, body)
	}
	form.Set("parameters", `[1]`)
	response, body = call(t, app, "POST", "/sql", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 422 || !strings.Contains(body, "parameters must be a JSON object") {
		t.Fatal(response.StatusCode, body)
	}
	response, _ = call(t, app, "POST", base+"/quality", url.Values{"action": {"capture_contract"}}.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if response.StatusCode != 403 {
		t.Fatal("missing CSRF accepted", response.StatusCode)
	}
}

func TestImportPreviewCreatesNoCollection(t *testing.T) {
	app, s, _ := setup(t, "", "")
	response, body := call(t, app, "GET", "/collections", "", nil, nil)
	_, before, _ := s.List("", 1, 100)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("csrf", csrf(t, body))
	writer.WriteField("csv_delimiter", "semicolon")
	writer.WriteField("preview_only", "on")
	part, _ := writer.CreateFormFile("file", "customers.csv")
	io.WriteString(part, "id;name\n001;Renée\n")
	writer.Close()
	response, body = call(t, app, "POST", "/collections/import", buf.String(), map[string]string{"Content-Type": writer.FormDataContentType()}, response.Cookies())
	if response.StatusCode != 200 || !strings.Contains(body, "No files were imported") || !strings.Contains(body, "001") || !strings.Contains(body, "Renée") {
		t.Fatal(response.StatusCode, body)
	}
	_, after, _ := s.List("", 1, 100)
	if before != after {
		t.Fatal("preview created a collection", before, after)
	}
}

func TestEngineeringAPIAccessAndDocumentation(t *testing.T) {
	app, _, col := setup(t, "", "secret")
	base := "/api/collections/" + col.ID
	for _, item := range []struct{ method, path, body string }{{"GET", base + "/contract", ""}, {"POST", base + "/compare", `{"other":"` + col.ID + `","keys":["name"]}`}, {"POST", base + "/validation-export", `{"mode":"separate"}`}} {
		r, _ := call(t, app, item.method, item.path, item.body, map[string]string{"Content-Type": "application/json"}, nil)
		if r.StatusCode != 401 {
			t.Fatal(item.path, r.StatusCode)
		}
	}
	headers := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer secret"}
	r, body := call(t, app, "POST", base+"/compare", `{"other":"`+col.ID+`","keys":["name"]}`, headers, nil)
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode, body)
	}
	r, body = call(t, app, "POST", "/api/sql", `{"collections":["`+col.ID+`"],"sql":"SELECT :n AS n","engine":"native","parameters":{"n":7}}`, headers, nil)
	var result struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || r.StatusCode != 200 || len(result.Rows) != 1 {
		t.Fatal(r.StatusCode, body, err)
	}
	r, body = call(t, app, "GET", "/api/openapi.json", "", headers, nil)
	for _, path := range []string{base + "/compare", base + "/contract", base + "/validation-export", "/api/pipelines/{pipeline}/audit"} {
		if !strings.Contains(body, path) {
			t.Fatal("missing OpenAPI operation", path)
		}
	}
}
