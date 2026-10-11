package conversion

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNestedFidelity(t *testing.T) {
	for _, input := range []string{
		`{"a":{"b":[{"c":[[1,2],[],{}],"text":"line one\nline two\t東京 <&>","nothing":null}]},"empty":""}`,
		`{"a":{"b":[{"id":184467440737095516160,"decimal":1.234567890123456789,"tiny":1e-1000}]}}`,
	} {
		want, err := Decode([]byte(input), "json")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"json", "yaml", "xml"} {
			b, err := Encode(want, f)
			if err != nil {
				t.Fatal(f, err)
			}
			got, err := Decode(b, f)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("%s changed values: got %#v want %#v", f, got, want)
			}
		}
	}
}

func TestScalarDatesRemainText(t *testing.T) {
	for _, tc := range []struct{ format, input, want string }{
		{"yaml", "date: 2024-01-01", "2024-01-01"},
		{"yaml", "date: 2024-01-01T12:34:56.123456789+02:00", "2024-01-01T12:34:56.123456789+02:00"},
		{"toml", "date = 2024-01-01", "2024-01-01"},
		{"toml", "date = 12:34:56.123456789", "12:34:56.123456789"},
	} {
		v, e := Decode([]byte(tc.input), tc.format)
		if e != nil {
			t.Fatal(e)
		}
		if v.(map[string]any)["date"] != tc.want {
			t.Errorf("%s got %#v want %s", tc.format, v, tc.want)
		}
	}
}

func TestRejectAmbiguousJSON(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`, `{"\u0078":1,"x":2}`, `"\ud800"`, `"\udc00"`, string([]byte{'"', 0xff, '"'})} {
		if _, e := Decode([]byte(input), "json"); e == nil {
			t.Errorf("accepted ambiguous input %q", input)
		}
	}
}

func TestStreamingCSVRejectsDroppedFields(t *testing.T) {
	var b bytes.Buffer
	w, e := NewRowWriter(&b, "csv", []string{"id"})
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Write(map[string]any{"id": 1, "extra": "keep me"}); e == nil {
		t.Fatal("dropped unlisted field")
	}
}

func TestUTF8BOM(t *testing.T) {
	for _, tc := range []struct{ format, input string }{{"json", `[{"id":1}]`}, {"csv", "id\n1\n"}, {"ndjson", "{\"id\":1}\n"}, {"yaml", "id: 1"}, {"toml", "id=1"}, {"xml", "<id>1</id>"}} {
		input := append([]byte{0xef, 0xbb, 0xbf}, []byte(tc.input)...)
		if _, e := Decode(input, tc.format); e != nil {
			t.Errorf("%s BOM: %v", tc.format, e)
		}
		count := 0
		e := Iterate(context.Background(), bytes.NewReader(input), tc.format, 1024, 4096, func(any) error { count++; return nil })
		if e != nil || count != 1 {
			t.Errorf("stream %s BOM: %d %v", tc.format, count, e)
		}
	}
}

func TestTwoHundredLevels(t *testing.T) {
	input := strings.Repeat(`{"x":`, 200) + `9007199254740993` + strings.Repeat("}", 200)
	v, e := Decode([]byte(input), "json")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 200; i++ {
		v = v.(map[string]any)["x"]
	}
	if v != json.Number("9007199254740993") {
		t.Fatal(v)
	}
}
