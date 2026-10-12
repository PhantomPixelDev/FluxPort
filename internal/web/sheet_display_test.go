package web

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"fluxport/internal/collections"
)

func TestSheetPresentationKeepsCompleteValues(t *testing.T) {
	tests := []struct {
		value     any
		exists    bool
		kind, raw string
	}{{nil, false, "missing", ""}, {nil, true, "null", "null"}, {"", true, "empty", ""}, {"001", true, "text", "001"}, {json.Number("900719925474099312345"), true, "number", "900719925474099312345"}, {true, true, "boolean", "true"}, {map[string]any{"n": json.Number("9007199254740993")}, true, "json", `{"n":9007199254740993}`}}
	for _, test := range tests {
		got := sheetCell(test.value, test.exists)
		if got.Kind != test.kind || got.Raw != test.raw {
			t.Fatalf("%#v became %#v", test, got)
		}
	}
	text := strings.Repeat("山", 600)
	got := sheetCell(text, true)
	if got.Raw != text || len([]rune(got.Text)) != 257 {
		t.Fatal("full Unicode value was lost")
	}
	if sheetType("string") != "Text" || sheetType("number, null") != "Number" || sheetType("number, string") != "Mixed" {
		t.Fatal("incorrect schema label")
	}
}

func TestSheetHeaderSortPreservesFiltersAndLiteralNames(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("sort.json", strings.NewReader(`[{"id":1,"a.b/x~y":2,"active":true},{"id":2,"a.b/x~y":1,"active":true},{"id":3,"a.b/x~y":0,"active":false}]`))
	if err != nil {
		t.Fatal(err)
	}
	original := collections.Query{Page: 2, Limit: 20, Filters: []collections.Filter{{Field: "active", Op: "eq", Value: true}}, Sort: []collections.Sort{{Field: "id"}}}
	first := sheetColumn("a.b/x~y", "number", col.ID, original)
	uri, err := url.Parse(first.SortURL)
	if err != nil {
		t.Fatal(err)
	}
	q, err := decodeQuery(uri.Query().Get("query"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Page != 1 || len(q.Filters) != 1 || len(q.Sort) != 2 || q.Sort[1].Field != "/a.b~1x~0y" {
		t.Fatalf("query changed: %#v", q)
	}
	second := sheetColumn("a.b/x~y", "number", col.ID, q)
	uri, _ = url.Parse(second.SortURL)
	q, _ = decodeQuery(uri.Query().Get("query"))
	if !q.Sort[1].Desc || second.Rank != 2 {
		t.Fatal("descending cycle failed")
	}
	third := sheetColumn("a.b/x~y", "number", col.ID, q)
	uri, _ = url.Parse(third.SortURL)
	q, _ = decodeQuery(uri.Query().Get("query"))
	if len(q.Sort) != 1 || q.Sort[0].Field != "id" {
		t.Fatal("clear removed other sorting")
	}
	sorted := collections.Query{Limit: 20, Filters: original.Filters, Sort: []collections.Sort{{Field: "/a.b~1x~0y"}}}
	result, err := s.Query(context.Background(), col, sorted)
	if err != nil || len(result.Records) != 2 || result.Records[0].Value.(map[string]any)["id"].(json.Number).String() != "2" {
		t.Fatal("literal column sort failed", err)
	}
	resp, body := call(t, app, "GET", first.SortURL, "", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `aria-label="Sort a.b/x~y"`) {
		t.Fatal("native sort link failed", resp.StatusCode)
	}
}

func TestSheetShowsEmptyColumnsAndValueStates(t *testing.T) {
	app, s, _ := setup(t, "", "")
	col, err := s.Import("states.json", strings.NewReader(`[{"id":1,"empty":"","maybe":null,"nested":{"n":9007199254740993}},{"id":2,"empty":""}]`))
	if err != nil {
		t.Fatal(err)
	}
	resp, body := call(t, app, "GET", "/collections/"+col.ID, "", nil, nil)
	for _, marker := range []string{`aria-label="Sort empty"`, `data-display-kind="empty"`, `data-display-kind="missing"`, `data-display-kind="null"`, `data-display-kind="json"`, "9007199254740993"} {
		if resp.StatusCode != 200 || !strings.Contains(body, marker) {
			t.Fatal("missing presentation state", marker)
		}
	}
	csv, err := s.Import("strings.csv", strings.NewReader("id,value\n001,2.5\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, body = call(t, app, "GET", "/collections/"+csv.ID, "", nil, nil)
	if strings.Contains(body, `data-type="Number"`) {
		t.Fatal("CSV strings inferred for display")
	}
}
