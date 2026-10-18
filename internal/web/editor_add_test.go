package web

import (
	"strings"
	"testing"
)

// Task 5 of docs/superpowers/plans/2026-10-17-ui-ux-simplification.md: the
// editor panel offers a plain per-column add-record form built from the
// sheet's visible columns, while the raw JSON textarea stays as the advanced
// path. Booleans render as checkboxes, not JSON.
func TestEditorPanelOffersPlainAddRecordForm(t *testing.T) {
	app, _, col := setup(t, "", "")
	page, html := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	if page.StatusCode != 200 {
		t.Fatal(page.StatusCode)
	}
	if !strings.Contains(html, "data-add-fields") {
		t.Fatal("mini add-record form missing")
	}
	if !strings.Contains(html, `data-field-name="name"`) || !strings.Contains(html, `data-field-name="active"`) {
		t.Fatal("add-record form is not built from the sheet columns")
	}
	if !strings.Contains(html, `data-field-name="active" type="checkbox"`) {
		t.Fatal("boolean column did not render a plain checkbox")
	}
	if !strings.Contains(html, `name="command_value"`) {
		t.Fatal("advanced raw JSON path missing")
	}
}
