package quality

import (
	"encoding/json"
	"testing"
)

func TestLiteralContractAndMissingNullEmpty(t *testing.T) {
	c := &Contract{Columns: []ContractColumn{{Name: "a.b/c", Types: []string{"string"}, Required: true}, {Name: "n", Types: []string{"integer"}, Nullable: true}}}
	v, err := NewContractValidator(nil, c)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{map[string]any{"a.b/c": "", "n": json.Number("9007199254740993123")}, map[string]any{"a.b/c": "Ada", "n": nil}, map[string]any{"a": map[string]any{"b/c": "wrong"}}, map[string]any{"a.b/c": nil, "extra": true}} {
		if err = v.Check(row); err != nil {
			t.Fatal(err)
		}
	}
	if v.Report.Invalid != 2 || v.Report.Errors != 4 {
		t.Fatal(v.Report)
	}
	c.Columns = append(c.Columns, c.Columns[0])
	if err = c.Validate(); err == nil {
		t.Fatal("duplicate column accepted")
	}
}
