package web

import (
	"fluxport/internal/config"
	"strings"
	"testing"
)

func TestPublicDemoAPIHasNoBearerRequirement(t *testing.T) {
	_, s, col := setup(t, "", "private-key")
	app, err := New(config.Config{Address: "127.0.0.1:8080", MaxFileBytes: 4096, PublicDemo: true, APIKey: "private-key"}, s)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/collections", "/api/collections/" + col.ID, "/api/collections/" + col.ID + "/schema", "/api/openapi.json", "/api/jobs"} {
		resp, body := call(t, app, "GET", path, "", nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("anonymous demo %s: %d %s", path, resp.StatusCode, body)
		}
	}
	resp, body := call(t, app, "GET", "/", "", nil, nil)
	if resp.StatusCode != 200 || strings.Contains(body, "Bearer protected") {
		t.Fatal("demo dashboard still advertises bearer protection")
	}
	// Existing private deployments must continue rejecting anonymous requests.
	private, _, _ := setup(t, "", "private-key")
	resp, _ = call(t, private, "GET", "/api/collections", "", nil, nil)
	if resp.StatusCode != 401 {
		t.Fatal("private API no longer requires its key")
	}
}
