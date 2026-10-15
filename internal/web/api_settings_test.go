package web

import (
	"encoding/json"
	"fluxport/internal/config"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIControls(t *testing.T) {
	app, s, col := setup(t, "", "")
	response, body := call(t, app, "GET", "/settings", "", nil, nil)
	cookies := response.Cookies()
	if !strings.Contains(body, `name="read_limit" min="1" max="1000000" value="120"`) {
		t.Fatal("missing valid initial API settings")
	}
	token := csrf(t, body)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	form := url.Values{"csrf": {token}, "rate_enabled": {"on"}, "read_limit": {"2"}, "write_limit": {"1"}, "window_seconds": {"60"}, "default_page_size": {"1"}, "max_page_size": {"1"}, "origins": {"https://example.com"}}
	response, body = call(t, app, "POST", "/settings/api", form.Encode(), headers, cookies)
	if response.StatusCode != 303 {
		t.Fatal(response.StatusCode, body)
	}
	path := "/api/collections/" + col.ID
	for i := 0; i < 2; i++ {
		response, body = call(t, app, "GET", path, "", map[string]string{"Origin": "https://example.com"}, nil)
		if response.StatusCode != 200 || response.Header.Get("Access-Control-Allow-Origin") != "https://example.com" {
			t.Fatal(response.StatusCode, body)
		}
		var result []map[string]any
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 {
			t.Fatal(result)
		}
	}
	response, body = call(t, app, "GET", path, "", nil, nil)
	if response.StatusCode != 429 || response.Header.Get("Retry-After") == "" || response.Header.Get("X-RateLimit-Remaining") != "0" {
		t.Fatal(response.StatusCode, body)
	}
	response, _ = call(t, app, "GET", "/settings", "", nil, cookies)
	if response.StatusCode != 200 {
		t.Fatal("dashboard incorrectly limited")
	}
	response, _ = call(t, app, "OPTIONS", path, "", map[string]string{"Origin": "https://example.com", "Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "Authorization"}, nil)
	if response.StatusCode != 204 {
		t.Fatal("preflight", response.StatusCode)
	}
	response, _ = call(t, app, "OPTIONS", path, "", map[string]string{"Origin": "https://evil.com", "Access-Control-Request-Method": "GET"}, nil)
	if response.StatusCode != 403 {
		t.Fatal("unapproved origin", response.StatusCode)
	}
	// Disable limiting; caps remain active for both query interfaces.
	form.Del("rate_enabled")
	response, _ = call(t, app, "POST", "/settings/api", form.Encode(), headers, cookies)
	if response.StatusCode != 303 {
		t.Fatal(response.StatusCode)
	}
	for _, request := range []struct {
		method, path, body string
		headers            map[string]string
	}{
		{"GET", path + "?limit=2", "", nil},
		{"POST", path + "/query", `{"limit":2}`, map[string]string{"Content-Type": "application/json"}},
	} {
		response, body = call(t, app, request.method, request.path, request.body, request.headers, nil)
		if response.StatusCode != 400 {
			t.Fatal(response.StatusCode, body)
		}
	}
	response, body = call(t, app, "POST", path+"/query", `{}`, map[string]string{"Content-Type": "application/json"}, nil)
	if response.StatusCode != 200 || !strings.Contains(body, `"limit":1`) {
		t.Fatal(response.StatusCode, body)
	}
	// Restarting the app retains policy from existing metadata.
	restarted, err := New(config.Config{Address: "127.0.0.1:8080", DataDir: filepath.Join(t.TempDir(), "unused"), MaxFileBytes: 4096}, s)
	if err != nil {
		t.Fatal(err)
	}
	response, body = call(t, restarted, "GET", path+"?limit=2", "", nil, nil)
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode, body)
	}
	form.Set("max_page_size", "500")
	response, _ = call(t, app, "POST", "/settings/api", form.Encode(), headers, cookies)
	if response.StatusCode != 400 {
		t.Fatal("invalid settings accepted")
	}
	form.Set("max_page_size", "1")
	form.Del("csrf")
	response, _ = call(t, app, "POST", "/settings/api", form.Encode(), headers, cookies)
	if response.StatusCode != 403 {
		t.Fatal("CSRF not enforced")
	}
}
func TestCollectionAPIControlsAndAuth(t *testing.T) {
	app, _, col := setup(t, "", "secret")
	response, body := call(t, app, "GET", "/collections/"+col.ID+"?tab=configuration", "", nil, nil)
	cookies := response.Cookies()
	token := csrf(t, body)
	form := url.Values{"csrf": {token}, "rate_enabled": {"on"}, "read_limit": {"1"}, "write_limit": {"1"}, "window_seconds": {"60"}, "default_page_size": {"0"}, "max_page_size": {"0"}}
	response, body = call(t, app, "POST", "/collections/"+col.ID+"/api-settings", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "X-Fluxport-View": "workspace"}, cookies)
	if response.StatusCode != 200 || !strings.Contains(body, "API controls saved.") || strings.Contains(body, "<!doctype") {
		t.Fatal(response.StatusCode, body)
	}
	path := "/api/collections/" + col.ID
	response, _ = call(t, app, "GET", path, "", map[string]string{"Authorization": "Bearer secret"}, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	response, _ = call(t, app, "GET", path, "", map[string]string{"Authorization": "Bearer secret", "X-Forwarded-For": "spoofed"}, nil)
	if response.StatusCode != 429 {
		t.Fatal("spoofed header bypassed limit", response.StatusCode)
	}
	response, _ = call(t, app, "GET", "/api/collections", "", nil, nil)
	if response.StatusCode != 401 {
		t.Fatal("auth bypassed", response.StatusCode)
	}
}
