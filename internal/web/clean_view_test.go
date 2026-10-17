package web

import (
	"net/url"
	"strings"
	"testing"
)

// Task 3: the Clean view names its outcome, asks for rows in plain words, and
// states exactly what the preview shows.
func TestCleanViewGuidesAndStaysHonest(t *testing.T) {
	app, _, col := setup(t, "", "")
	base := "/collections/" + col.ID
	page, body := call(t, app, "GET", base+"/changes", "", nil, nil)
	if page.StatusCode != 200 {
		t.Fatal(page.StatusCode)
	}
	if !strings.Contains(body, "<h2>Change your file</h2>") || !strings.Contains(body, "Pick a change, preview it, then apply it to the file.") {
		t.Fatal("outcome heading missing")
	}
	if strings.Contains(body, "Rows to clean") {
		t.Fatal("old Rows to clean label remains")
	}
	if !strings.Contains(body, "Filtered rows</option>") || !strings.Contains(body, ">All rows</option>") || !strings.Contains(body, `value="matching"`) || !strings.Contains(body, `value="entire"`) {
		t.Fatal("row scope wording changed or option values broken")
	}
	if !strings.Contains(body, "No steps yet — build one above.") {
		t.Fatal("empty steps guidance missing")
	}
	genAt := strings.Index(body, `data-source-generation="`)
	gen := ""
	if genAt >= 0 {
		rest := body[genAt+len(`data-source-generation="`):]
		if end := strings.Index(rest, `"`); end >= 0 {
			gen = rest[:end]
		}
	}
	form := url.Values{"csrf": {csrf(t, body)}, "action": {"preview"}, "scope": {"entire"}, "file_mode": {"1"}, "source_generation": {gen}}
	form.Add("steps", `{"op":"transform","transform":{"kind":"trim","field":"name"}}`)
	resp, body := call(t, app, "POST", base+"/changes", form.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, page.Cookies())
	if resp.StatusCode != 200 {
		at := strings.Index(body, "notice error")
		snippet := body
		if at >= 0 && len(body) > at+160 {
			snippet = body[at : at+160]
		}
		t.Fatalf("preview POST got %d: %s", resp.StatusCode, snippet)
	}
	if !strings.Contains(body, "Previewing the first 20 rows of the result.") || !strings.Contains(body, "prepared in the background") {
		t.Fatal("sample banner is not honest about coverage")
	}
	if !strings.Contains(body, ">Apply to file</button>") {
		t.Fatal("apply button wording missing after building steps")
	}
}
