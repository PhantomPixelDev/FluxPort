package transformations

import (
	"reflect"
	"testing"
)

func TestRulesDoNotMutateSource(t *testing.T) {
	src := []any{map[string]any{"name": "Ada", "active": true, "secret": "x"}, map[string]any{"name": "Grace", "active": false}}
	r, err := Parse(`{"select":["name","active"],"drop":["active"],"rename":{"name":"display_name"},"filter":{"active":true}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := Apply(src, r)
	want := []any{map[string]any{"display_name": "Ada"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if src[0].(map[string]any)["name"] != "Ada" {
		t.Fatal("mutated source")
	}
}
func TestRenameSwapDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := Apply([]any{map[string]any{"a": 1, "b": 2}}, Rules{Rename: map[string]string{"a": "b", "b": "a"}})
		if got[0].(map[string]any)["a"] != 2 || got[0].(map[string]any)["b"] != 1 {
			t.Fatal(got)
		}
	}
}
