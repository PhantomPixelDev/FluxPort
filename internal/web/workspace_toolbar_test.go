package web

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// Task 2 of docs/superpowers/plans/2026-10-17-ui-ux-simplification.md: the file
// toolbar renders four primary tabs (Data, Clean, SQL, Summary) and a single
// ⋯ menu holding every other file control. The standalone "File settings"
// toolbar link is gone; the menu keeps the data-workspace-theme mountpoint JS
// uses (file-workspace.js:126-131).
func TestWorkspaceToolbarFourTabsAndFileMenu(t *testing.T) {
	app, _, col := setup(t, "", "")
	base := "/collections/" + col.ID
	resp, html := call(t, app, "GET", base, "", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	for _, label := range []string{"Data", "Clean", "SQL", "Summary"} {
		if !strings.Contains(html, ">"+label+"</a>") {
			t.Fatalf("primary tab %q missing", label)
		}
	}
	if strings.Contains(html, "Inspect</a>") || strings.Contains(html, "Inspect data") {
		t.Fatal("old Inspect wording remains")
	}
	if strings.Contains(html, "workspace-file-action") {
		t.Fatal("standalone File settings link remains in toolbar")
	}
	if !strings.Contains(html, `aria-label="More file options"`) {
		t.Fatal("⋯ menu has no More file options label")
	}
	menuStart := strings.Index(html, "workspace-extra-tools")
	menuEnd := strings.Index(html, "</details>")
	if menuStart < 0 || menuEnd < 0 {
		t.Fatal("workspace-more menu missing")
	}
	menu := html[menuStart : strings.Index(html[menuStart:], "</details>")+menuStart]
	if !strings.Contains(menu, `<div data-workspace-theme>`) {
		t.Fatal("menu lost the data-workspace-theme mountpoint")
	}
	order := []string{"File history", "File settings", "API settings", "Saved views", "Processing history"}
	last := 0
	for _, want := range order {
		at := strings.Index(menu, want)
		if at < 0 {
			t.Fatalf("menu item %q missing", want)
		}
		if at < last {
			t.Fatalf("menu item %q out of order", want)
		}
		last = at
	}
}

// Exactly one primary tab marks the current view with aria-current="page", so
// screen readers announce position in the workspace. The Summary tab covers its
// whole view group (Summary, Checks, Schema, JSON); overlays such as File
// settings or Convert / Download mark no tab.
func TestWorkspaceTabMarksActiveView(t *testing.T) {
	app, _, col := setup(t, "", "")
	base := "/collections/" + col.ID
	for _, view := range []struct {
		path   string
		target string
	}{
		{base, `data-workspace-data`},
		{base + "/changes", `data-workspace-target="clean"`},
		{base + "/sql", `data-workspace-target="sql"`},
		{base + "/insights", `data-workspace-target="insights"`},
		{base + "/quality", `data-workspace-target="insights"`},
		{base + "?tab=schema", `data-workspace-target="insights"`},
	} {
		resp, html := call(t, app, "GET", view.path, "", nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", view.path, resp.StatusCode)
		}
		start := strings.Index(html, `aria-label="Views for this file"`)
		if start < 0 {
			t.Fatalf("%s: workspace navigation missing", view.path)
		}
		toolbar := html[start:]
		if end := strings.Index(toolbar, "</nav>"); end > 0 {
			toolbar = toolbar[:end]
		}
		if n := strings.Count(toolbar, `aria-current="page"`); n != 1 {
			t.Fatalf("%s: %d primary tabs marked current, want 1", view.path, n)
		}
		marked := false
		for _, link := range strings.Split(toolbar, "<a ") {
			if strings.Contains(link, view.target) && strings.Contains(link, `aria-current="page"`) {
				marked = true
			}
		}
		if !marked {
			t.Fatalf("%s: %s does not carry aria-current", view.path, view.target)
		}
	}
}

// Followups section 5: every column header carries one menu offering explicit
// sort directions, Filter, Hide, Copy values (and Rename on editable files),
// so per-column actions no longer live in three separate toolbars.
func TestColumnHeaderMenu(t *testing.T) {
	app, _, col := setup(t, "", "")
	resp, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	headStart := strings.Index(html, "<thead")
	if headStart < 0 {
		t.Fatal("table head missing")
	}
	head := html[headStart:]
	if end := strings.Index(head, "</thead>"); end > 0 {
		head = head[:end]
	}
	menus := strings.Count(head, `class="column-menu"`)
	columns := strings.Count(head, "<th data-sheet-cell=")
	if menus != columns || columns == 0 {
		t.Fatalf("%d column menus for %d columns", menus, columns)
	}
	for _, want := range []string{"Sort ascending", "Sort descending", "Filter", "Hide column", "Copy values"} {
		if !strings.Contains(head, ">"+want) && !strings.Contains(head, `aria-label="`+want+`"`) {
			t.Fatalf("column menu is missing %q", want)
		}
	}
	menu := head[strings.Index(head, `class="column-menu"`):]
	menu = menu[:strings.Index(menu, "</details>")]
	for _, want := range []string{"data-column-hide", "data-column-copy"} {
		if !strings.Contains(menu, want) {
			t.Fatalf("column menu missing %s action", want)
		}
	}
	if strings.Contains(html, "editor-column-menu") {
		t.Fatal("old standalone per-column edit button remains")
	}
}
// which strict CSP only allows through its sha256 source. This test hashes the
// script exactly as served and requires the Content-Security-Policy header to
// carry it, so editing the script without updating the header fails here.
func TestInlineThemeScriptAllowedByCSP(t *testing.T) {
	app, _, _ := setup(t, "", "")
	resp, html := call(t, app, "GET", "/collections", "", nil, nil)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	start := strings.Index(html, "<script>try{var t=localStorage.getItem('fluxport-theme')")
	if start < 0 {
		t.Fatal("inline theme script missing from the head")
	}
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatal("inline theme script is not closed")
	}
	script := html[start+len("<script>") : start+end]
	sum := sha256.Sum256([]byte(script))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, hash) {
		t.Fatalf("script-src does not allow the inline theme script: add %s to the header, got %s", hash, csp)
	}
}

// The ⋯ menu is the only carrier of file controls after Task 2 removed the
// standalone toolbar link, so hiding it at desktop width strands File
// settings, File history and API settings (CI browser check May 2026 hit
// exactly this: settings link resolved but never visible). The workspace CSS
// must keep .workspace-more rendered at desktop and must not keep selectors
// for the removed .workspace-file-action element.
func TestWorkspaceMenuVisibleOnDesktop(t *testing.T) {
	css, err := os.ReadFile("../../ui/static/css/workspace.css")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(css), ".workspace-more {")
	if start < 0 {
		t.Fatal("base .workspace-more rule missing")
	}
	rest := string(css[start:])
	baseRule := rest[:strings.IndexAny(rest, "}")+1]
	if strings.Contains(baseRule, "display: none") {
		t.Fatal("⋯ menu hidden at desktop; File settings unreachable")
	}
	if strings.Contains(string(css), ".workspace-file-action") {
		t.Fatal("dead .workspace-file-action CSS remains")
	}
}
