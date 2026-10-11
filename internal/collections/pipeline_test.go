package collections

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/transformations"
)

func runPipelineTest(t *testing.T, s *Service, p Pipeline) []any {
	t.Helper()
	text, ids, err := CompilePipeline(p)
	if err != nil {
		t.Fatal(err)
	}
	out := []any{}
	err = s.SQLStream(context.Background(), ids, text, func(_ []string, row []any) error {
		v, e := conversion.Decode([]byte(row[0].(string)), "json")
		if e != nil {
			return e
		}
		out = append(out, v)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestPipelineFilterMapRenameSortDeduplicatePreservesTypes(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1,"name":"Ada?","active":true,"large":9007199254740993,"nested":{"v":true}},{"id":2,"name":"Grace","active":false},{"id":3,"name":"Ada?","active":true,"large":9007199254740993,"nested":{"v":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	p := Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "filter", Filters: []Filter{{Field: "name", Op: "eq", Value: "Ada?"}}}, {Op: "rename", Rules: transformations.Rules{Rename: map[string]string{"name": "display_name"}}}, {Op: "map", Set: map[string]any{"new": json.Number("9007199254740995")}, Copy: map[string]string{"copied": "nested.v"}}, {Op: "sort", Sort: []Sort{{Field: "id", Desc: true}}}, {Op: "deduplicate", Keys: []string{"display_name"}}}}
	rows := runPipelineTest(t, s, p)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	row := rows[0].(map[string]any)
	if row["id"] != json.Number("3") || row["display_name"] != "Ada?" || row["active"] != true || row["copied"] != true || row["large"] != json.Number("9007199254740993") || row["new"] != json.Number("9007199254740995") {
		t.Fatal("pipeline changed values or types", row)
	}
	if _, exists := row["name"]; exists {
		t.Fatal("rename left old field")
	}
}
func TestPipelineGroupingAndJoins(t *testing.T) {
	s := testService(t)
	users, err := s.Import("users.json", strings.NewReader(`[{"id":1,"name":"Ada"},{"id":2,"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	orders, err := s.Import("orders.json", strings.NewReader(`[{"user_id":1,"amount":2},{"user_id":1,"amount":3},{"user_id":2,"amount":9}]`))
	if err != nil {
		t.Fatal(err)
	}
	rows := runPipelineTest(t, s, Pipeline{Source: orders.ID, Steps: []PipelineStep{{Op: "group", Keys: []string{"user_id"}, Aggregates: []Aggregate{{Op: "sum", Field: "amount", As: "total"}, {Op: "count", As: "orders"}}}, {Op: "join", Join: &PipelineJoin{Collection: users.ID, Left: "user_id", Right: "id", As: "customer"}}, {Op: "sort", Sort: []Sort{{Field: "total", Desc: true}}}}})
	if len(rows) != 2 || rows[0].(map[string]any)["customer"].(map[string]any)["name"] != "Grace" || rows[1].(map[string]any)["total"] != json.Number("5") {
		t.Fatal(rows)
	}
}

func TestPipelineProjectionMatchesExistingRulesAndPreservesNull(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"a":1,"b":2,"null":null,"flag":true,"nested":{"v":[1,true,null]}}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, rules := range []transformations.Rules{{Select: []string{"b"}, Rename: map[string]string{"a": "b"}}, {Drop: []string{"a"}, Rename: map[string]string{"a": "b"}}, {Rename: map[string]string{"null": "nothing", "a": "b", "b": "c"}}, {Select: []string{"flag", "null", "nested"}}} {
		original, err := s.Read(c)
		if err != nil {
			t.Fatal(err)
		}
		expected := transformations.Apply(original.([]any), rules)
		actual := runPipelineTest(t, s, Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "rename", Rules: rules}}})
		if differences := conversion.Differences(expected, actual); len(differences) > 0 {
			t.Fatal("rule semantics differ", differences, actual)
		}
	}
}

func TestPipelinePersistenceBackgroundFormatsAndNumericParameters(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":9007199254740993,"active":true},{"id":9007199254740992,"active":false}]`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePipeline(Pipeline{Name: "Exact numbers", Source: c.ID, Steps: []PipelineStep{{Op: "filter", Filters: []Filter{{Field: "id", Op: "eq", Value: json.Number("9007199254740993")}}}, {Op: "map", Set: map[string]any{"new": json.Number("9007199254740995")}}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.GetPipeline(p.ID)
	if err != nil || p.Steps[1].Set["new"] != json.Number("9007199254740995") {
		t.Fatal(p, err)
	}
	if err = s.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	waitCollection(t, s, c.ID, "ready")
	for _, format := range conversion.Formats {
		j, e := s.PipelineJob(p, format)
		if e != nil {
			t.Fatal(e)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			j, _ = s.Jobs.Get(j.ID)
			if j.State == "ready" || j.State == "failed" {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if j.State != "ready" || j.Records != 1 {
			t.Fatal(format, j)
		}
		path, e := s.Download(j.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		v, e := conversion.Decode(b, format)
		if e != nil {
			t.Fatal(format, e)
		}
		if format == "json" {
			row := v.([]any)[0].(map[string]any)
			if row["id"] != json.Number("9007199254740993") || row["new"] != json.Number("9007199254740995") {
				t.Fatal("background job lost numeric precision", row)
			}
		}
	}
}
