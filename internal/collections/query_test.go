package collections

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestQueryFiltersSortIdentityAndAtomicBulk(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1,"name":"Ada","team":{"name":"blue"},"score":2},{"id":2,"name":"Grace","team":{"name":"blue"},"score":10},{"id":3,"name":"Lin","team":{"name":"red"},"score":5}]`))
	if err != nil {
		t.Fatal(err)
	}
	q := Query{Filters: []Filter{{Field: "team.name", Op: "eq", Value: "blue"}}, Sort: []Sort{{Field: "score", Desc: true}}, Limit: 1}
	first, err := s.Query(context.Background(), c, q)
	if err != nil || first.Total != 2 || len(first.Records) != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	if first.Records[0].Value.(map[string]any)["name"] != "Grace" {
		t.Fatal(first)
	}
	q.Page = 2
	second, err := s.Query(context.Background(), c, q)
	if err != nil || second.Records[0].ID == first.Records[0].ID {
		t.Fatal(second, err)
	}
	if err = s.Update(c.ID, true, true, ""); err != nil {
		t.Fatal(err)
	}
	_, err = s.BulkMutate(c.ID, first.Generation, []Mutation{{ID: first.Records[0].ID, Method: "PATCH", Value: map[string]any{"name": "Changed", "id": json.Number("9007199254740993")}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Query(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if after.Records[1].ID != first.Records[0].ID {
		t.Fatal("ID changed on edit", after)
	}
	if after.Records[1].Value.(map[string]any)["id"] != json.Number("9007199254740993") {
		t.Fatal("integer precision lost")
	}
	if _, err = s.BulkMutate(c.ID, first.Generation, []Mutation{{ID: first.Records[0].ID, Method: "DELETE"}}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit accepted", err)
	}
	before, _ := os.ReadFile(c.Path)
	if _, err = s.BulkMutate(c.ID, after.Generation, []Mutation{{ID: after.Records[0].ID, Method: "DELETE"}, {ID: "missing", Method: "DELETE"}}); err == nil {
		t.Fatal("partial invalid batch accepted")
	}
	unchanged, _ := os.ReadFile(c.Path)
	if string(before) != string(unchanged) {
		t.Fatal("invalid batch changed source")
	}
	_, err = s.BulkMutate(c.ID, after.Generation, []Mutation{{ID: after.Records[0].ID, Method: "DELETE"}, {ID: after.Records[2].ID, Method: "PATCH", Value: map[string]any{"score": 11}}})
	if err != nil {
		t.Fatal(err)
	}
	final, err := s.Query(context.Background(), c, Query{})
	if err != nil || final.Total != 2 || final.Records[0].ID != after.Records[1].ID || final.Records[1].ID != after.Records[2].ID {
		t.Fatalf("%+v %v", final, err)
	}
}

func TestIdentityDuplicatesReorderingAndSavedViews(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1},{"id":1},{"name":"Ada"},{"name":"Grace"}]`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Query(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Records[0].ID == a.Records[1].ID {
		t.Fatal("duplicate identities")
	}
	if err = os.WriteFile(c.Path, []byte(`[{"name":"Grace"},{"name":"Ada"},{"id":1},{"id":1}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncFile(c.Path); err != nil {
		t.Fatal(err)
	}
	b, err := s.Query(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Records[0].ID != a.Records[3].ID || b.Records[1].ID != a.Records[2].ID || b.Records[2].ID != a.Records[0].ID {
		t.Fatal("identities changed on reorder")
	}
	view, err := s.SaveView(View{CollectionID: c.ID, Name: "Named", Query: Query{Filters: []Filter{{Field: "name", Op: "notnull"}}, Page: 99}, Columns: []string{"name"}})
	if err != nil {
		t.Fatal(err)
	}
	view.Query.Sort = []Sort{{Field: "name", Desc: true}}
	_, err = s.SaveView(view)
	if err != nil {
		t.Fatal(err)
	}
	views, err := s.Views(c.ID)
	if err != nil || len(views) != 1 || views[0].Query.Page != 1 || len(views[0].Query.Sort) != 1 {
		t.Fatal(views, err)
	}
	if err = s.DeleteView(c.ID, view.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQueryValidationAndInjection(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"name":"Ada"}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []Query{{Filters: []Filter{{Field: "name", Op: "DROP TABLE"}}}, {Sort: []Sort{{Field: `x"');DROP TABLE records;--`}}}, {Filters: []Filter{{Field: "name", Op: "eq", Value: []any{1}}}}} {
		if _, err = s.Query(context.Background(), c, q); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
	result, err := s.Query(context.Background(), c, Query{Filters: []Filter{{Field: "name", Op: "eq", Value: `' OR 1=1 --`}}})
	if err != nil || result.Total != 0 {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Query(ctx, c, Query{}); err == nil {
		t.Fatal("cancelled query ran")
	}
}

func TestTypedFiltersAndNestedFieldEdits(t *testing.T) {
	s := testService(t)
	c, err := s.Import("types.json", strings.NewReader(`[{"v":true,"nested":{"value":1},"large":9007199254740993},{"v":1},{"v":"1"},{"v":null},{}]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{true, json.Number("1"), "1"} {
		result, e := s.Query(context.Background(), c, Query{Filters: []Filter{{Field: "v", Op: "eq", Value: value}}})
		if e != nil || result.Total != 1 {
			t.Fatalf("%v: %+v %v", value, result, e)
		}
	}
	result, err := s.Query(context.Background(), c, Query{Filters: []Filter{{Field: "v", Op: "neq", Value: true}}})
	if err != nil || result.Total != 4 {
		t.Fatal(result, err)
	}
	all, err := s.Query(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(c.ID, true, true, ""); err != nil {
		t.Fatal(err)
	}
	_, err = s.BulkMutate(c.ID, all.Generation, []Mutation{{ID: all.Records[0].ID, Method: "FIELD", Value: map[string]any{"field": "nested.value", "value": json.Number("9007199254740995")}}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.Query(context.Background(), c, Query{})
	if err != nil {
		t.Fatal(err)
	}
	row := after.Records[0].Value.(map[string]any)
	if row["large"] != json.Number("9007199254740993") || row["nested"].(map[string]any)["value"] != json.Number("9007199254740995") {
		t.Fatal("field edit lost precision", row)
	}
	// A source append after an edit must retain the edited record's identity.
	b, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(b))
	text = strings.TrimSuffix(text, "]") + `,{"v":"new"}]`
	if err = os.WriteFile(c.Path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncFile(c.Path); err != nil {
		t.Fatal(err)
	}
	latest, err := s.Query(context.Background(), c, Query{})
	if err != nil || latest.Records[0].ID != all.Records[0].ID {
		t.Fatal("edited ID lost on source append", latest, err)
	}
}

func TestSavedViewPreservesExactNumericFilter(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":9007199254740993},{"id":9007199254740992}]`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveView(View{CollectionID: c.ID, Name: "Exact identifier", Query: Query{Filters: []Filter{{Field: "id", Op: "eq", Value: json.Number("9007199254740993")}}}})
	if err != nil {
		t.Fatal(err)
	}
	views, err := s.Views(c.ID)
	if err != nil || views[0].Query.Filters[0].Value != json.Number("9007199254740993") {
		t.Fatal("saved filter lost precision", views, err)
	}
	result, err := s.Query(context.Background(), c, views[0].Query)
	if err != nil || result.Total != 1 || result.Records[0].Ordinal != 0 {
		t.Fatal(result, err)
	}
}
