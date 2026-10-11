package quality

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestValidationPrecisionNestedAndMissing(t *testing.T) {
	min := json.Number("9007199254740993")
	length := 2
	v, err := NewValidator([]Rule{{Field: "id", Type: "integer", Min: &min, Unique: true}, {Field: "/a~1b/0/name", Required: true, MinLength: &length}, {Field: "date", DateLayout: "2006-01-02"}, {Field: "x", Required: true, NonNull: true}})
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"id": json.Number("9007199254740992"), "a/b": []any{map[string]any{"name": "你好"}}, "date": "2024-02-30", "x": nil}
	if err = v.Check(row); err != nil {
		t.Fatal(err)
	}
	if v.Report.Errors != 3 || v.Report.Invalid != 1 {
		t.Fatal(v.Report)
	}
	if err = v.Check(row); err != nil {
		t.Fatal(err)
	}
	if v.Report.Errors != 7 {
		t.Fatal(v.Report)
	}
	if _, err = NewValidator([]Rule{{Field: "x", Pattern: "["}}); err == nil {
		t.Fatal("invalid regex accepted")
	}
}
func TestProfilePrecisionCoverageAndCaps(t *testing.T) {
	p := NewProfiler()
	for i := 0; i < 1100; i++ {
		row := map[string]any{"id": json.Number(fmt.Sprint(9007199254740993 + int64(i))), "text": "你好"}
		if i == 0 {
			row["nullable"] = nil
		}
		if err := p.Add(row); err != nil {
			t.Fatal(err)
		}
	}
	cols := p.Columns()
	if cols[0].Min != "9007199254740993" || !cols[0].DistinctCapped || cols[0].Mean != "9007199254741542.50000000" {
		t.Fatal(cols)
	}
	if cols[1].Null != 1 || cols[1].Missing != 1099 || *cols[2].MinLength != 2 {
		t.Fatal(cols)
	}
}
func TestValidationIssueAndUniqueBounds(t *testing.T) {
	if _, ok := Number(json.Number("1e999999999")); ok {
		t.Fatal("unbounded exponent accepted")
	}
	v, _ := NewValidator([]Rule{{Field: "id", Required: true}})
	for i := 0; i < 1000; i++ {
		if err := v.Check(map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(v.Report.Issues) != 200 || v.Report.Errors != 1000 || !v.Report.IssuesTruncated {
		t.Fatal(v.Report)
	}
	v, _ = NewValidator([]Rule{{Field: "id", Unique: true}})
	for i := 0; i < 10000; i++ {
		if err := v.Check(map[string]any{"id": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Check(map[string]any{"id": 10000}); err == nil {
		t.Fatal("unbounded uniqueness")
	}
}
