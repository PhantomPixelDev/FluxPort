package collections

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"fluxport/internal/transformations"
)

func TestPipelineNativeCleanupOrderingAppendLimit(t *testing.T) {
	s := testService(t)
	c, err := s.Import("messy.json", strings.NewReader(`[{"id":1,"name":" Ada ","price":9007199254740993.125},{"id":2,"name":" Grace ","price":2}]`))
	if err != nil {
		t.Fatal(err)
	}
	p := Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}, {Op: "transform", Transform: &transformations.Operation{Kind: "multiply", Field: "price", Value: json.Number("1.2")}}, {Op: "filter", Filters: []Filter{{Field: "name", Op: "eq", Value: "Ada"}}}, {Op: "append", Append: c.ID}, {Op: "limit", Limit: 2}}}
	rows, err := s.PreviewPipeline(context.Background(), p, -1)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if rows[0].(map[string]any)["price"] != json.Number("10808639105689191.75") || rows[1].(map[string]any)["name"] != " Ada " {
		t.Fatal(rows)
	}
	p.Steps = []PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "cast_number", Field: "name"}}}
	if _, err = s.PreviewPipeline(context.Background(), p, -1); err == nil {
		t.Fatal("invalid cast accepted")
	}
}
