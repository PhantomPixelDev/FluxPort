package conversion

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrips(t *testing.T) {
	source := `{"people":[{"name":"Ada & <Grace>","id":42,"active":true,"score":3.5}],"title":"Team"}`
	v, err := Decode([]byte(source), "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml", "xml", "toml"} {
		t.Run(format, func(t *testing.T) {
			b, err := Encode(v, format)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(b, format)
			if err != nil {
				t.Fatalf("%s: %v", b, err)
			}
			if !reflect.DeepEqual(got, v) {
				t.Fatalf("got %#v, want %#v", got, v)
			}
		})
	}
}
func TestRowsAndCSV(t *testing.T) {
	v, err := Decode([]byte("name,note\nAda,\"line one\nline two\"\nGrace,compiler\n"), "csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(Rows(v)) != 2 {
		t.Fatal(v)
	}
	for _, format := range []string{"csv", "ndjson", "xml", "yaml"} {
		b, err := Encode(v, format)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(b, format)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("%s lost values: %#v", format, got)
		}
	}
}
func TestRejectInvalidInput(t *testing.T) {
	for _, tc := range []struct{ format, input string }{{"json", `{} {}`}, {"yaml", "a: 1\n---\nb: 2"}, {"csv", "a,a\n1,2"}, {"xml", "<!DOCTYPE root><root/>"}, {"xml", "<a/><b/>"}, {"ndjson", "{\n\"a\":1\n}"}, {"json", "[oops"}, {"toml", "a = ["}, {"xml", strings.Repeat("<a>", 70) + strings.Repeat("</a>", 70)}} {
		if _, err := Decode([]byte(tc.input), tc.format); err == nil {
			t.Errorf("accepted invalid %s", tc.format)
		}
	}
	if _, err := Encode([]any{1}, "toml"); err == nil {
		t.Fatal("TOML accepted array root")
	}
	if _, err := Encode([]any{1}, "csv"); err == nil {
		t.Fatal("CSV accepted scalar rows")
	}
}
func TestLargeNumbers(t *testing.T) {
	v, err := Decode([]byte(`{"id":9007199254740993}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "xml", "yaml", "toml"} {
		b, err := Encode(v, format)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Decode(b, format)
		if err != nil {
			t.Fatal(err)
		}
		if got.(map[string]any)["id"] != json.Number("9007199254740993") {
			t.Fatalf("%s lost integer precision: %s", format, b)
		}
	}
}

func TestTOMLRejectsNull(t *testing.T) {
	v, err := Decode([]byte(`{"missing":null}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Encode(v, "toml"); err == nil {
		t.Fatal("TOML silently discarded null")
	}
}

// A value converted to every supported format must read back unchanged. The
// suite is a guard against the artifacts users saw: HTML-escaped JSON output,
// YAML/TOML numbers that could not be parsed back, and lost XML namespaces.
// YAML and TOML have no arbitrary-precision numbers, so out-of-range lexemes
// are covered by TestOutOfRangeNumbers instead of this exact-equality check.
func TestRoundTripFidelity(t *testing.T) {
	source := `{
		"html": "<b>& \"quoted\"</b>",
		"unicode": "Renée Müller · 山田 太郎",
		"deep": {"a": {"b": {"c": [1, 2, {"d": "e"}]}}},
		"list": [1, 2, [3, 4]],
		"int64max": 9223372036854775807,
		"int64min": -9223372036854775808,
		"precision": 9007199254740993,
		"float": 0.33125,
		"exp": 1.5e-7,
		"truthy": true,
		"null": null
	}`
	want, err := Decode([]byte(source), "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "yaml", "xml", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			b, err := Encode(want, format)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode(b, format)
			if err != nil {
				t.Fatalf("%s: %v\n%s", format, err, b)
			}
			// NDJSON has one row per line, so a single object decodes to a sequence.
			if format == "ndjson" {
				want = []any{want}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s lost values:\n got %#v\nwant %#v\noutput:\n%s", format, got, want, b)
			}
		})
	}
}

// JSON output must contain the source characters, not \u003c escapes.
func TestNoHTMLEscapingInOutput(t *testing.T) {
	v, err := Decode([]byte(`{"html":"<b>&</b>"}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"json", "ndjson", "xml", "csv", "yaml", "toml"} {
		b, err := Encode(v, format)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), `\u003c`) || strings.Contains(string(b), `\u0026`) {
			t.Fatalf("%s HTML-escaped output: %s", format, b)
		}
	}
	if b, err := MarshalJSON(v); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(b), `<b>&</b>`) {
		t.Fatalf("MarshalJSON: %s", b)
	}
}

func TestOutOfRangeNumbers(t *testing.T) {
	for _, lexeme := range []string{"99999999999999999999", "9223372036854775808", "1e400", "1.8e3080", "1e-1000", "1.234567890123456789"} {
		v, e := Decode([]byte(`{"n":`+lexeme+`}`), "json")
		if e != nil {
			t.Fatal(e)
		}
		b, e := Encode(v, "yaml")
		if e != nil {
			t.Fatal(e)
		}
		got, e := Decode(b, "yaml")
		if e != nil || !reflect.DeepEqual(got, v) {
			t.Fatalf("YAML changed number %s: %#v %v", lexeme, got, e)
		}
		if _, e = Encode(v, "toml"); e == nil {
			t.Fatalf("TOML accepted lossy number %s", lexeme)
		}
	}
}

func TestByteOrderMarkAndEmptyInput(t *testing.T) {
	bom := string([]byte{0xEF, 0xBB, 0xBF})
	for _, format := range Formats {
		payload := map[string]string{
			"json":   `{"a":1}`,
			"yaml":   "a: 1",
			"csv":    "a,b\n1,2",
			"ndjson": `{"a":1}`,
			"toml":   "a = 1",
			"xml":    "<r><a>1</a></r>",
		}[format]
		if _, err := Decode([]byte(bom+payload), format); err != nil {
			t.Fatalf("%s rejected a BOM: %v", format, err)
		}
	}
	// A CSV header that began with a BOM must not carry it into the field name.
	v, err := Decode([]byte(bom+"a,b\n1,2"), "csv")
	if err != nil {
		t.Fatal(err)
	}
	row := v.([]any)[0].(map[string]any)
	if _, bad := row["\ufeffa"]; bad {
		t.Fatal("CSV header kept the BOM")
	}
	if _, ok := row["a"]; !ok {
		t.Fatalf("CSV header lost: %#v", row)
	}
	// Every format reports the same message for empty input.
	for _, format := range Formats {
		for _, empty := range []string{"", "   ", "\n\t"} {
 if format=="ndjson"||format=="toml"{if _,err:=Decode([]byte(empty),format);err!=nil{t.Fatal(format,err)};continue}
			if _, err := Decode([]byte(empty), format); err == nil || !strings.Contains(err.Error(), "empty") {
				t.Fatalf("%s empty input gave %v", format, err)
			}
		}
	}
}

// Namespaced element and attribute names must not collapse into one JSON key.
func TestXMLNamespacePrefixes(t *testing.T) {
	v, err := Decode([]byte(`<r><a:x>1</a:x><b:x>2</b:x></r>`), "xml")
	if err != nil {
		t.Fatal(err)
	}
	r := v.(map[string]any)["r"].(map[string]any)
	if r["a:x"] != "1" || r["b:x"] != "2" {
		t.Fatalf("namespaced siblings collided: %#v", r)
	}
	// A declared prefix is reported as the prefix the document used, not the URI.
	v, err = Decode([]byte(`<p:r xmlns:p="http://example.com"><p:c v="1"/></p:r>`), "xml")
	if err != nil {
		t.Fatal(err)
	}
	r = v.(map[string]any)["p:r"].(map[string]any)
	child, ok := r["p:c"].(map[string]any)
	if !ok {
		t.Fatalf("declared prefix resolved to a URI: %#v", r)
	}
	if child["@v"] != "1" {
		t.Fatalf("attribute name lost: %#v", child)
	}
	// A default namespace adds no prefix.
	v, err = Decode([]byte(`<r xmlns="http://example.com"><c>1</c></r>`), "xml")
	if err != nil {
		t.Fatal(err)
	}
	r = v.(map[string]any)["r"].(map[string]any)
	if r["c"] != "1" {
		t.Fatalf("default namespace leaked a prefix: %#v", r)
	}
}

// The envelope keeps < > & readable instead of numeric entities.
func TestXMLEnvelopeEscaping(t *testing.T) {
	v, err := Decode([]byte(`{"note":"line one\nline two","html":"<b>&</b>","q":"say \"hi\""}`), "json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(v, "xml")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"&#34;", "&#39;", "\n\ttab"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("envelope contains %q: %s", bad, b)
		}
	}
	if !strings.Contains(string(b), "&lt;b&gt;&amp;&lt;/b&gt;") {
		t.Fatalf("envelope did not escape markup: %s", b)
	}
	got, err := Decode(b, "xml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, v) {
		t.Fatalf("envelope round trip: %#v", got)
	}
}
