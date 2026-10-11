package transformations

import (
	"encoding/json"
	"strings"
	"testing"

	"fluxport/internal/conversion"
)

func TestOperationsFidelityAndUnicode(t *testing.T) {
	row, _ := conversion.Decode([]byte(`{"name":"  ÄDA你好  ","price":9007199254740993.125,"code":"0012","date":"11/10/2026","tags":"a|b"}`), "json")
	for _, tc := range []struct {
		op   Operation
		want any
	}{{Operation{Kind: "trim", Field: "name"}, "ÄDA你好"}, {Operation{Kind: "lower", Field: "name"}, "  äda你好  "}, {Operation{Kind: "substring", Field: "name", Start: 3, Length: 4}, "DA你好"}, {Operation{Kind: "multiply", Field: "price", Value: json.Number("1.2")}, json.Number("10808639105689191.75")}, {Operation{Kind: "date", Field: "date", Layout: "02/01/2006", OutputLayout: "2006-01-02"}, "2026-10-11"}} {
		got, err := ApplyOperation(row, tc.op)
		if err != nil {
			t.Fatal(err)
		}
		if got.(map[string]any)[tc.op.Field] != tc.want {
			t.Fatal(tc.op, got)
		}
	}
	if _, err := ApplyOperation(row, Operation{Kind: "cast_number", Field: "code"}); err == nil {
		t.Fatal("leading zeros silently changed")
	}
	if _, err := ApplyOperation(map[string]any{"x": json.Number("1e999999999")}, Operation{Kind: "add", Field: "x", Value: json.Number("1")}); err == nil {
		t.Fatal("unbounded exponent")
	}
	if row.(map[string]any)["name"] != "  ÄDA你好  " {
		t.Fatal("source mutated")
	}
}
func TestFlattenAndSplit(t *testing.T) {
	if _, err := ApplyOperation(map[string]any{"a.b": 1, "a": map[string]any{"b": 2}}, Operation{Kind: "flatten"}); err == nil {
		t.Fatal("collision ignored")
	}
	got, err := ApplyOperation(map[string]any{"a": map[string]any{"b": json.Number("123456789012345678901234567890")}}, Operation{Kind: "flatten"})
	if err != nil || got.(map[string]any)["a.b"] != json.Number("123456789012345678901234567890") {
		t.Fatal(got, err)
	}
	got, err = ApplyOperation(map[string]any{"tags": "a|你好|c"}, Operation{Kind: "split", Field: "tags", Delimiter: "|"})
	if err != nil || len(got.(map[string]any)["tags"].([]any)) != 3 {
		t.Fatal(got, err)
	}
}

func TestReplacementAmplificationBounds(t *testing.T) {
	row := map[string]any{"text": strings.Repeat("a", 128<<10)}
	for _, op := range []Operation{{Kind: "replace", Field: "text", Find: "", Replacement: strings.Repeat("x", 4096)}, {Kind: "regex", Field: "text", Find: "(.*)", Replacement: strings.Repeat("$1", 2048)}, {Kind: "split", Field: "text", Delimiter: "a"}} {
		if _, err := ApplyOperation(row, op); err == nil {
			t.Fatal("amplification bound ignored", op.Kind)
		}
	}
	got, err := ApplyOperation(map[string]any{"text": "Ada 123"}, Operation{Kind: "regex", Field: "text", Find: `(\w+) (\d+)`, Replacement: "${2}:$1:$$"})
	if err != nil || got.(map[string]any)["text"] != "123:Ada:$" {
		t.Fatal(got, err)
	}
}
