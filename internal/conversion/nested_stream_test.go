package conversion

import (
	"bytes"
	"testing"
)

func TestNestedTOMLStream(t *testing.T) {
	var b bytes.Buffer
	w, e := NewRowWriter(&b, "toml", nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = w.Write(map[string]any{"id": i, "address": map[string]any{"city": "東京"}, "tags": []any{"one", "two"}}); e != nil {
			t.Fatal(e)
		}
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	v, e := Decode(b.Bytes(), "toml")
	if e != nil {
		t.Fatal(e)
	}
	rows := v.(map[string]any)["records"].([]any)
	if len(rows) != 3 || rows[2].(map[string]any)["address"].(map[string]any)["city"] != "東京" {
		t.Fatal(v)
	}
}
