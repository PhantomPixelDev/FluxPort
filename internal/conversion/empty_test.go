package conversion

import (
	"reflect"
	"testing"
)

func TestEmptyValuesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		format string
		value  any
	}{{"ndjson", []any{}}, {"toml", map[string]any{}}} {
		b, e := Encode(tc.value, tc.format)
		if e != nil {
			t.Fatal(e)
		}
		v, e := Decode(b, tc.format)
		if e != nil || !reflect.DeepEqual(v, tc.value) {
			t.Fatal(tc.format, v, e)
		}
	}
	for _, input := range []string{`"\ud83d\ude00"`, `"\\ud800"`} {
		v, e := Decode([]byte(input), "json")
		if e != nil {
			t.Fatal(input, e)
		}
		b, e := Encode(v, "json")
		if e != nil {
			t.Fatal(e)
		}
		got, e := Decode(b, "json")
		if e != nil || got != v {
			t.Fatal(input, got, e)
		}
	}
}
