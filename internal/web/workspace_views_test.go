package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestFocusedWorkspaceNativeAndFragmentContracts(t *testing.T) {
	app, _, col := setup(t, "", "")
	base := "/collections/" + col.ID
	for _, item := range []struct{ path, view string }{
		{base, "data"}, {base + "/changes", "clean"}, {base + "/sql", "sql"},
		{base + "/insights", "insights"}, {base + "/quality", "quality"},
		{base + "?tab=schema", "schema"}, {base + "?tab=raw", "raw"},
		{base + "?tab=configuration", "api"}, {base + "?tab=downloads", "export"},
	} {
		t.Run(item.view, func(t *testing.T) {
			native, html := call(t, app, "GET", item.path, "", nil, nil)
			if native.StatusCode != 200 || !strings.Contains(html, `collection-workspace`) || !strings.Contains(html, `data-initial-view="`+item.view+`"`) {
				t.Fatal("Native view lacks the collection shell", native.StatusCode)
			}
			if strings.Contains(html, `file-workspace-grid`) || strings.Contains(html, `<aside class="file-workspace-tool"`) {
				t.Fatal("Old sidebar workspace remains")
			}
			fragment, body := call(t, app, "GET", item.path, "", map[string]string{"X-Fluxport-View": "workspace"}, nil)
			if fragment.StatusCode != 200 || !strings.Contains(body, `data-workspace-fragment="`+item.view+`"`) || strings.Contains(body, "<!doctype") || strings.Contains(body, `class="sidebar"`) {
				t.Fatal("Workspace fragment contains a standalone page", fragment.StatusCode)
			}
			if !strings.Contains(fragment.Header.Get("Vary"), "X-Fluxport-View") {
				t.Fatal("Missing fragment cache variance")
			}
		})
	}
	legacy, html := call(t, app, "GET", base, "", map[string]string{"HX-Request": "true"}, nil)
	if legacy.StatusCode != 200 || !strings.Contains(html, `class="detail-body"`) || strings.Contains(html, "<!doctype") {
		t.Fatal("Existing HTMX data contract changed")
	}
}

func TestWorkspaceErrorsStayInlineAndKeepCSRF(t *testing.T) {
	app, _, col := setup(t, "", "")
	route := "/collections/" + col.ID + "/sql"
	headers := map[string]string{"X-Fluxport-View": "workspace", "X-Fluxport-Workspace": "1", "HX-Request": "true", "Content-Type": "application/x-www-form-urlencoded"}
	form := url.Values{"sql": {"SELECT 1"}, "action": {"run"}}
	response, body := call(t, app, "POST", route, form.Encode(), headers, nil)
	if response.StatusCode != 403 || strings.Contains(body, "<!doctype") {
		t.Fatal("Fragment requests bypass CSRF or return full error pages", response.StatusCode)
	}
}
