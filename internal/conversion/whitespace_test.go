package conversion

import (
	"bytes"
	"reflect"
	"testing"
)

func TestYAMLStreamBlockScalarsAndLeadingSpaces(t *testing.T) {
	rows := []any{map[string]any{"nested": map[string]any{"text": "  first\nsecond\n", "more": []any{map[string]any{"note": "   x\n y"}}}}, "  first\nsecond\n", []any{"  x\ny", ""}, nil}
	var b bytes.Buffer
	w, e := NewRowWriter(&b, "yaml", nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range rows {
		if e = w.Write(row); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	v, e := Decode(b.Bytes(), "yaml")
	if e != nil || !reflect.DeepEqual(v, rows) {
		t.Fatalf("block scalar changed: %#v %v\n%s", v, e, b.Bytes())
	}
}

func TestHeaderOnlyCSVAndWhitespace(t *testing.T) {
	input := []byte("id;name\n")
	o := Options{CSVDelimiter: "auto"}
	v, e := DecodeWithOptions(input, "csv", o)
	if e != nil {
		t.Fatal(e)
	}
	b, e := EncodeConversion(v, input, "csv", "csv", o)
	if e != nil || string(b) != "id,name\n" {
		t.Fatal(string(b), e)
	}
	raw := map[string]any{"text": "line1\r\nline2"}
	b, e = EncodeWithOptions(raw, "csv", Options{SpreadsheetSafe: true})
	if e != nil || !bytes.Contains(b, []byte("line1\r\nline2")) {
		t.Fatal("export normalized newlines", string(b), e)
	}
	v, e = Decode([]byte("<root><leaf>  hello  </leaf></root>"), "xml")
	if e != nil || v.(map[string]any)["root"].(map[string]any)["leaf"] != "  hello  " {
		t.Fatal(v, e)
	}
}

func TestTOMLImportRejectsPrecisionLoss(t *testing.T) {
	for _, s := range []string{"x=1.234567890123456789", "x={nested=[{value=1e-1000}]}", "x=nan", "x=inf"} {
		if _, e := Decode([]byte(s), "toml"); e == nil {
			t.Fatal("rounded TOML literal", s)
		}
	}
	for _, s := range []string{"x=0.1", "x=+1.5e-7", "x=0.0e9999999999999999999999"} {
		if _, e := Decode([]byte(s), "toml"); e != nil {
			t.Fatal(s, e)
		}
	}
}
