package web

import (
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
