package web

import (
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
	menu := html[menuStart:strings.Index(html[menuStart:], "</details>")+menuStart]
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
