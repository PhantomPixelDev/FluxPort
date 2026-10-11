package conversion

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCSVInferenceAndDelimiters(t *testing.T) {
	for _, delimiter := range []string{",", ";", "\t", "|"} {
		input := strings.Join([]string{"id", "price", "enabled", "missing", "empty", "large"}, delimiter) + "\n" + strings.Join([]string{"00123", "2.50", "true", "null", "", "9007199254740993"}, delimiter) + "\n"
		options := Options{CSVDelimiter: "auto", CSVInferTypes: true}
		v, e := DecodeWithOptions([]byte(input), "csv", options)
		if e != nil {
			t.Fatal(delimiter, e)
		}
		want := map[string]any{"id": "00123", "price": json.Number("2.50"), "enabled": true, "missing": nil, "empty": "", "large": json.Number("9007199254740993")}
		if !reflect.DeepEqual(v.([]any)[0], want) {
			t.Fatalf("%q: %#v", delimiter, v)
		}
		e = IterateWithOptions(context.Background(), strings.NewReader(input), "csv", 1024, 4096, options, func(v any) error {
			if !reflect.DeepEqual(v, want) {
				t.Fatalf("stream %#v", v)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"+2", "01", "-01", "", " ", " 1", "1 ", "NaN", "Inf", "1,234", "2024-01-01", "123-456", "[1]", "{\"x\":1}"} {
		if csvScalar(s, true) != s {
			t.Errorf("changed text %q", s)
		}
	}
	if _, e := DecodeWithOptions([]byte("a,b;c\n1,2;3\n"), "csv", Options{CSVDelimiter: "auto"}); e == nil {
		t.Fatal("ambiguous delimiter accepted")
	}
}

func TestSpreadsheetSafeCSV(t *testing.T) {
	source := map[string]any{"=header": "=cmd|'/C calc'!A0", "negative": json.Number("-42"), "text": "  @SUM(1)", "nested": map[string]any{"x": 1}}
	b, e := EncodeWithOptions(source, "csv", Options{SpreadsheetSafe: true})
	if e != nil {
		t.Fatal(e)
	}
	v, e := Decode(b, "csv")
	if e != nil {
		t.Fatal(e)
	}
	row := v.([]any)[0].(map[string]any)
	if row["'=header"] != "'=cmd|'/C calc'!A0" || row["text"] != "'  @SUM(1)" || row["negative"] != "-42" || row["nested"] != `{"x":1}` {
		t.Fatal(row)
	}
	var stream bytes.Buffer
	w, e := NewRowWriterWithOptions(&stream, "csv", []string{"=header", "negative", "nested", "text"}, Options{SpreadsheetSafe: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Write(source); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(stream.Bytes(), b) {
		t.Fatalf("stream differs:\n%s\n%s", stream.Bytes(), b)
	}
	raw, e := Encode(source, "csv")
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Equal(raw, b) {
		t.Fatal("spreadsheet-safe option ineffective")
	}
	for _, fields := range [][]string{{""}, {"x", "x"}} {
		if _, e := NewRowWriter(&bytes.Buffer{}, "csv", fields); e == nil {
			t.Fatal("invalid headers accepted")
		}
	}
}

func TestXMLMixedContentAndPrefixes(t *testing.T) {
	v, e := Decode([]byte(`<root>hello <child>c1</child> world <![CDATA[<&>]]><child>c2</child>!</root>`), "xml")
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"root": map[string]any{"#content": []any{map[string]any{"#text": "hello "}, map[string]any{"child": "c1"}, map[string]any{"#text": " world <&>"}, map[string]any{"child": "c2"}, map[string]any{"#text": "!"}}}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("lost order %#v", v)
	}
	for _, f := range []string{"json", "xml", "yaml"} {
		b, e := Encode(v, f)
		if e != nil {
			t.Fatal(e)
		}
		got, e := Decode(b, f)
		if e != nil || !reflect.DeepEqual(got, v) {
			t.Fatal(f, got, e)
		}
	}
	v, e = Decode([]byte(`<r xmlns:a="urn:same" xmlns:b="urn:same"><a:x>1</a:x><b:x>2</b:x></r>`), "xml")
	if e != nil {
		t.Fatal(e)
	}
	obj := v.(map[string]any)["r"].(map[string]any)
	if obj["a:x"] != "1" || obj["b:x"] != "2" {
		t.Fatal(obj)
	}
	for _, s := range []string{`<a><b></a></b>`, `<a x="1" x="2"/>`, `<fluxport encoding="json"><child/>{"x":1}</fluxport>`} {
		if _, e := Decode([]byte(s), "xml"); e == nil {
			t.Fatal("accepted malformed XML", s)
		}
	}
}

func TestYAMLAliasesMergesAndRejections(t *testing.T) {
	v, e := Decode([]byte("defaults: &defaults\n  id: 9007199254740993\n  active: true\nfirst:\n  <<: *defaults\n  active: false\nsecond:\n  <<: [*defaults, {name: Example}]\n"), "yaml")
	if e != nil {
		t.Fatal(e)
	}
	m := v.(map[string]any)
	if m["first"].(map[string]any)["active"] != false || m["second"].(map[string]any)["id"] != json.Number("9007199254740993") {
		t.Fatal(v)
	}
	for _, s := range []string{"x: 1\nx: 2", "x: .nan", "x: .inf", "1: value", "x: &x [*x]", "x: !custom value"} {
		if _, e := Decode([]byte(s), "yaml"); e == nil {
			t.Fatal("accepted incompatible YAML", s)
		}
	}
}

func TestNestedSequenceStreamFidelity(t *testing.T) {
	v, e := Decode([]byte(`[{"id":1,"nested":{"list":[{"items":[{"x":1},{"x":2}]},{"items":[]}]},"note":"first\nsecond\n"},{"id":2,"nested":{"list":[[1,2],[]]},"note":"x"}]`), "json")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range []string{"json", "yaml", "xml", "ndjson", "toml"} {
		var b bytes.Buffer
		w, e := NewRowWriter(&b, f, nil)
		if e != nil {
			t.Fatal(e)
		}
		for _, row := range Rows(v) {
			if e = w.Write(row); e != nil {
				t.Fatal(e)
			}
		}
		if e = w.Close(); e != nil {
			t.Fatal(e)
		}
		got, e := Decode(b.Bytes(), f)
		if e != nil {
			t.Fatalf("%s: %v\n%s", f, e, b.Bytes())
		}
		if f == "toml" {
			got = got.(map[string]any)["records"]
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("%s changed nesting:\n%s\ngot %#v", f, b.Bytes(), got)
		}
	}
}
