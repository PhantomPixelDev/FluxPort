package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"fluxport/internal/collections"
	"fluxport/internal/config"
	"fluxport/internal/database"
	"github.com/gofiber/fiber/v3"
)

func setup(t *testing.T, password, key string) (*fiber.App, *collections.Service, collections.Collection) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{Address: "127.0.0.1:8080", DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), MaxFileBytes: 4096, AdminPassword: password, APIKey: key}
	db, err := database.Open(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := collections.New(db, cfg.DataDir, cfg.MaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	col, err := s.Import("people.json", strings.NewReader(`[{"name":"<script>alert(1)</script>","active":true},{"name":"Grace","active":false}]`))
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(cfg, s)
	if err != nil {
		t.Fatal(err)
	}
	return app, s, col
}
func call(t *testing.T, app *fiber.App, method, path, body string, headers map[string]string, cookies []*http.Cookie) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(b)
}
func csrf(t *testing.T, body string) string {
	t.Helper()
	matches := regexp.MustCompile(`<meta name="csrf-token" content="([a-f0-9]+)">`).FindStringSubmatch(body)
	if len(matches) != 2 {
		t.Fatal("missing CSRF token")
	}
	return matches[1]
}
func TestPagesFragmentsAndCaching(t *testing.T) {
	app, _, col := setup(t, "", "")
	for _, path := range []string{"/", "/collections", "/converter", "/explorer", "/settings", "/collections/" + col.ID, "/collections/" + col.ID + "?tab=schema", "/collections/" + col.ID + "?tab=configuration"} {
		resp, body := call(t, app, "GET", path, "", nil, nil)
		if resp.StatusCode != 200 || !strings.Contains(body, "<!doctype html>") {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatal("template did not escape source data")
		}
	}
	resp, body := call(t, app, "GET", "/collections?q=missing", "", map[string]string{"HX-Request": "true"}, nil)
	if resp.StatusCode != 200 || strings.Contains(body, "<!doctype") || !strings.Contains(body, "No matching collections") {
		t.Fatal(body)
	}
	resp, _ = call(t, app, "GET", "/static/js/htmx.min.js", "", nil, nil)
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == "" {
		t.Fatal("static asset caching missing")
	}
	resp, _ = call(t, app, "GET", "/static/js/htmx.min.js", "", map[string]string{"If-None-Match": resp.Header.Get("ETag")}, nil)
	if resp.StatusCode != 304 {
		t.Fatal(resp.StatusCode)
	}
}
func TestSessionAndCSRF(t *testing.T) {
	app, _, _ := setup(t, "secret", "")
	resp, _ := call(t, app, "GET", "/", "", nil, nil)
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/login" {
		t.Fatal(resp.StatusCode)
	}
	resp, body := call(t, app, "GET", "/login", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	resp, _ = call(t, app, "POST", "/login", "password=secret", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 403 {
		t.Fatal("login CSRF was not enforced")
	}
	resp, _ = call(t, app, "POST", "/login", "password=secret&csrf="+token, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cookies)
	if resp.StatusCode != 303 {
		t.Fatal(resp.StatusCode)
	}
	cookies = resp.Cookies()
	resp, body = call(t, app, "GET", "/settings", "", nil, cookies)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	token = csrf(t, body)
	resp, _ = call(t, app, "POST", "/settings", "watch_enabled=on", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true"}, cookies)
	if resp.StatusCode != 403 {
		t.Fatal("mutation CSRF not enforced")
	}
	resp, body = call(t, app, "POST", "/settings", "watch_enabled=on", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "Settings saved") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}
func TestConverterAndImport(t *testing.T) {
	app, s, _ := setup(t, "", "")
	resp, body := call(t, app, "GET", "/converter", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}
	form := url.Values{"input": {`{"count":42}`}, "from": {"json"}, "to": {"yaml"}}
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "count: 42") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	form.Set("input", "bad")
	resp, body = call(t, app, "POST", "/converter", form.Encode(), headers, cookies)
	if resp.StatusCode != 422 || !strings.Contains(body, "Parsing error") {
		t.Fatal(body)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	f, err := w.CreateFormFile("file", "uploaded.csv")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("name\nAda\n"))
	w.Close()
	headers["Content-Type"] = w.FormDataContentType()
	resp, _ = call(t, app, "POST", "/collections/import", buf.String(), headers, cookies)
	if resp.StatusCode != 200 || resp.Header.Get("HX-Trigger") != "collectionsChanged" {
		t.Fatal(resp.StatusCode)
	}
	_, total, err := s.List("", 1, 20)
	if err != nil || total != 2 {
		t.Fatalf("%d %v", total, err)
	}
}

// The converter must render the characters the user supplied. Go's JSON encoder
// HTML-escapes < > & by default, which showed up as \u003c and \u0026 in output.
func TestConverterPreservesMarkupAndMultiline(t *testing.T) {
	app, _, _ := setup(t, "", "")
	resp, body := call(t, app, "GET", "/converter", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}
	for _, to := range []string{"json", "yaml", "xml", "ndjson", "csv"} {
		input := `{"html":"<b>&</b>","note":"line one\nline two","list":[1,2]}`
		_, body := call(t, app, "POST", "/converter", url.Values{"input": {input}, "from": {"json"}, "to": {to}, "csrf": {token}}.Encode(), headers, cookies)
		// The template escapes for the browser; the value a user sees must be
		// the characters they typed, not \u003c or \u0026 escapes.
		if strings.Contains(body, `\u003c`) || strings.Contains(body, `\u0026`) {
			t.Fatalf("%s output was HTML-escaped: %s", to, body)
		}
		value := htmlUnescape(textareaValue(t, body))
		// XML character data cannot hold a literal < or &, so the envelope
		// escapes them; every other target writes them through unchanged.
		want := `<b>&</b>`
		if to == "xml" {
			want = `&lt;b&gt;&amp;&lt;/b&gt;`
		}
		if !strings.Contains(value, want) {
			t.Fatalf("%s output lost the value: %s", to, body)
		}
		if to != "csv" && to != "xml" && !strings.Contains(value, "line one") {
			t.Fatalf("%s output lost the second line: %s", to, body)
		}
	}
}

// Output rendered by the converter must be accepted as input by the same format.
func TestConverterRoundTripThroughHandler(t *testing.T) {
	app, _, _ := setup(t, "", "")
	resp, body := call(t, app, "GET", "/converter", "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}
	for _, to := range []string{"json", "yaml", "xml", "ndjson"} {
		_, rendered := call(t, app, "POST", "/converter", url.Values{
			"input": {`{"big":99999999999999999999,"deep":{"a":[1,{"b":"c"}]}}`},
			"from":  {"json"}, "to": {to}, "csrf": {token},
		}.Encode(), headers, cookies)
		value := htmlUnescape(textareaValue(t, rendered))
		resp, back := call(t, app, "POST", "/converter", url.Values{
			"input": {value}, "from": {to}, "to": {"json"}, "csrf": {token},
		}.Encode(), headers, cookies)
		if resp.StatusCode != 200 {
			t.Fatalf("%s output rejected as %s input: %d %s", to, to, resp.StatusCode, back)
		}
		if !strings.Contains(htmlUnescape(textareaValue(t, back)), "99999999999999999999") {
			t.Fatalf("%s round trip lost the value: %s", to, back)
		}
	}
}

// textareaValue pulls the rendered output out of the converter fragment.
func textareaValue(t *testing.T, body string) string {
	t.Helper()
	const marker = `<textarea id="convert-output"`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("no output panel rendered: %s", body)
	}
	start += len(marker)
	openEnd := strings.Index(body[start:], ">")
	if openEnd < 0 {
		t.Fatal("unterminated textarea opening tag")
	}
	start += openEnd + 1
	end := strings.Index(body[start:], "</textarea>")
	if end < 0 {
		t.Fatal("unterminated textarea")
	}
	return body[start : start+end]
}

// htmlUnescape reverses the entities Go's contextual escaper uses inside a
// textarea, so the recovered string is what the browser shows the user.
func htmlUnescape(s string) string {
	r := strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&")
	return r.Replace(s)
}
func TestAPIAuthPaginationNegotiationCRUD(t *testing.T) {
	app, s, col := setup(t, "admin", "api-secret")
	base := "/api/collections/" + col.ID
	resp, _ := call(t, app, "GET", base, "", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatal("API key not enforced")
	}
	h := map[string]string{"Authorization": "Bearer api-secret"}
	resp, body := call(t, app, "GET", base+"?limit=1&page=2", "", h, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-Total-Count") != "2" || !strings.Contains(body, "Grace") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	h["Accept"] = "application/xml;q=0.9, application/json;q=0.2"
	resp, body = call(t, app, "GET", base, "", h, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "<fluxport") {
		t.Fatal(body)
	}
	h["Accept"] = "image/png"
	resp, _ = call(t, app, "GET", base, "", h, nil)
	if resp.StatusCode != 406 {
		t.Fatal(resp.StatusCode)
	}
	delete(h, "Accept")
	resp, _ = call(t, app, "GET", base+"?limit=101", "", h, nil)
	if resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	}
	h["Content-Type"] = "application/json"
	resp, _ = call(t, app, "POST", base+"/rows", `{"name":"New"}`, h, nil)
	if resp.StatusCode != 405 {
		t.Fatal("CRUD enabled by default")
	}
	if err := s.Update(col.ID, true, true, ""); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", "/rows", `{"name":"New"}`, 201}, {"PATCH", "/rows/2", `{"name":"Updated"}`, 200}, {"PUT", "/rows/2", `{"name":"Replaced"}`, 200}, {"DELETE", "/rows/2", "", 204}} {
		resp, body = call(t, app, tc.method, base+tc.path, tc.body, h, nil)
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: %d %s", tc.method, resp.StatusCode, body)
		}
	}
	resp, body = call(t, app, "GET", "/api/openapi.json", "", h, nil)
	var doc map[string]any
	if resp.StatusCode != 200 || json.Unmarshal([]byte(body), &doc) != nil || doc["openapi"] != "3.0.3" {
		t.Fatal(body)
	}
	if err := s.Update(col.ID, false, false, ""); err != nil {
		t.Fatal(err)
	}
	resp, _ = call(t, app, "GET", base, "", h, nil)
	if resp.StatusCode != 404 {
		t.Fatal("disabled endpoint accessible")
	}
}

func TestConfigurationFragmentUpdatesStatus(t *testing.T) {
	app, s, col := setup(t, "", "")
	resp, body := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	cookies := resp.Cookies()
	token := csrf(t, body)
	resp, body = call(t, app, "POST", "/collections/"+col.ID+"/config", "rules=", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "HX-Request": "true", "X-CSRF-Token": token}, cookies)
	if resp.StatusCode != 200 || !strings.Contains(body, "hx-swap-oob") || !strings.Contains(body, "Disabled") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	got, err := s.Get(col.ID)
	if err != nil || got.Enabled {
		t.Fatalf("%+v %v", got, err)
	}
}
