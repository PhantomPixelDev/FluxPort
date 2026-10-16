package collections

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"fluxport/internal/quality"
)

func TestCompareExactKeysSchemaAndDuplicates(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	a, err := s.Import("before.json", strings.NewReader(`[{"key.with/slash":"001","n":9007199254740993123,"v":null},{"key.with/slash":"002","v":""},{"key.with/slash":"003","v":{"nested":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Import("after.json", strings.NewReader(`[{"key.with/slash":"001","n":9007199254740993124,"v":null},{"key.with/slash":"002","v":""},{"key.with/slash":"004","new":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(a.Path)
	report, err := s.Compare(ctx, a.ID, CompareRequest{Other: b.ID, Keys: []string{"key.with/slash"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 1 || report.Removed != 1 || report.Changed != 1 || report.Unchanged != 1 || len(report.AddedColumns) != 1 {
		t.Fatalf("%+v", report)
	}
	data, _ := json.Marshal(report.Sample)
	if !bytes.Contains(data, []byte("9007199254740993124")) {
		t.Fatal(string(data))
	}
	after, _ := os.ReadFile(a.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("comparison changed source")
	}
	dupe, err := s.Import("duplicate.json", strings.NewReader(`[{"id":1},{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Compare(ctx, dupe.ID, CompareRequest{Other: dupe.ID, Keys: []string{"id"}}); err == nil || !strings.Contains(err.Error(), "unique") {
		t.Fatal(err)
	}
	if _, err = s.Compare(ctx, a.ID, CompareRequest{Other: b.ID, Keys: []string{"missing"}}); err == nil {
		t.Fatal("missing key accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.Compare(cancelled, a.ID, CompareRequest{Other: b.ID, Keys: []string{"key.with/slash"}}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestContractAndPartitionedExport(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c, err := s.Import("records.json", strings.NewReader(`[{"id":9007199254740993123,"name":"Ada"},{"id":2,"name":null},{"id":3,"extra":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	contract := &quality.Contract{Columns: []quality.ContractColumn{{Name: "id", Types: []string{"integer"}, Required: true}, {Name: "name", Types: []string{"string"}, Required: true, Nullable: false}}}
	if err = s.SaveContract(c.ID, contract); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Contract(c.ID)
	if err != nil || saved == nil {
		t.Fatal(saved, err)
	}
	report, err := s.Quality(ctx, c, QualityRequest{Contract: saved})
	if err != nil || report.Validation.Invalid != 2 {
		t.Fatal(report, err)
	}
	job, err := s.QualityExportJob(c.ID, "separate")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.qualityExportJob(ctx, job, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries := map[string]string{}
	for _, entry := range z.File {
		r, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		entries[entry.Name] = string(b)
	}
	if !strings.Contains(entries["valid.ndjson"], "9007199254740993123") || strings.Count(entries["rejected.ndjson"], "\n") != 2 || !strings.Contains(entries["rejected.ndjson"], `"issues"`) {
		t.Fatal(entries)
	}
	stop, err := s.QualityExportJob(c.ID, "stop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.qualityExportJob(ctx, stop, func(int64, int64) {}); err == nil {
		t.Fatal("stop published invalid output")
	}
	s.DiskCheck = func(string, uint64) error { return errors.New("low disk") }
	if _, err = s.qualityExportJob(ctx, job, func(int64, int64) {}); err == nil {
		t.Fatal("disk check ignored")
	}
}

func TestInputOptionsGzipEncodingAndDelimiter(t *testing.T) {
	s := testService(t)
	var compressed bytes.Buffer
	g := gzip.NewWriter(&compressed)
	g.Write([]byte("id;name\n001;Ren\xe9\n"))
	g.Close()
	c, err := s.ImportInput(context.Background(), "people.csv.gz", &compressed, InputOptions{Encoding: "windows-1252", CSVDelimiter: "semicolon"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.PreviewPipeline(context.Background(), Pipeline{Source: c.ID}, -1)
	if err != nil {
		t.Fatal(err)
	}
	obj := rows[0].(map[string]any)
	if obj["id"] != "001" || obj["name"] != "René" || c.Format != "ndjson" {
		t.Fatal(rows, c.Format)
	}
	var original bytes.Buffer
	g = gzip.NewWriter(&original)
	g.Write([]byte("[{\"a\": 1}]\n"))
	g.Close()
	d, err := s.ImportInput(context.Background(), "data.json.gz", &original, InputOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(d.Path)
	if string(b) != "[{\"a\": 1}]\n" {
		t.Fatal(string(b))
	}
}

func TestRecipeParametersNestedOperationsAndAudit(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c, err := s.Import("nested.json", strings.NewReader(`[{"id":"001","items":[9007199254740993123,null,{"x":"東京"}],"jan":"10","feb":"20"}]`))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.PreviewPipeline(ctx, Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "explode", Nested: &NestedStep{Field: "items", As: "item"}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rows)
	if len(rows) != 3 || !bytes.Contains(b, []byte("9007199254740993123")) || !bytes.Contains(b, []byte(`"item":null`)) {
		t.Fatal(string(b))
	}
	p := Pipeline{Source: c.ID, Steps: []PipelineStep{{Op: "unpivot", Nested: &NestedStep{Columns: []string{"jan", "feb"}, As: "month", ValueAs: "amount"}}, {Op: "pivot", Keys: []string{"id"}, Nested: &NestedStep{Field: "month", Value: "amount", Columns: []string{"jan", "feb"}}}}}
	rows, err = s.PreviewPipeline(ctx, p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].(map[string]any)["jan"] != "10" {
		t.Fatal(rows)
	}
	param := Pipeline{Source: c.ID, Parameters: map[string]any{"category": "default"}, Steps: []PipelineStep{{Op: "map", Set: map[string]any{"category": map[string]any{"$parameter": "category"}}}}}
	bound, err := BindPipeline(param, map[string]any{"category": "O'Reilly 東京"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.PreviewPipeline(ctx, bound, 0)
	if err != nil || rows[0].(map[string]any)["category"] != "O'Reilly 東京" {
		t.Fatal(rows, err)
	}
	if _, err = BindPipeline(param, map[string]any{"unknown": 1}); err == nil {
		t.Fatal("unknown parameter accepted")
	}
	j, err := s.PipelineAuditJob(p)
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.pipelineAuditJob(ctx, j, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var audit PipelineAudit
	if err = json.Unmarshal(data, &audit); err != nil || len(audit.Steps) != 2 || audit.Steps[0].RowsIn != 1 || audit.Steps[0].RowsOut != 2 || audit.Steps[1].RowsOut != 1 {
		t.Fatal(string(data), err)
	}
	dupe, err := s.Import("duplicate-pivot.json", strings.NewReader(`[{"id":1,"month":"jan","amount":1},{"id":1,"month":"jan","amount":2}]`))
	if err != nil {
		t.Fatal(err)
	}
	p.Source = dupe.ID
	p.Steps = p.Steps[1:]
	if _, err = s.PreviewPipeline(ctx, p, 0); err == nil {
		t.Fatal("duplicate pivot cell accepted")
	}
}

func TestSQLParameterIsolationAndExplain(t *testing.T) {
	text, err := BindSQL(`SELECT :value, ':value', "name:quoted" -- :ignored
/* :ignored */ WHERE 1=:number`, map[string]any{"value": "'; DROP TABLE x; --", "number": json.Number("1")})
	if err != nil || !strings.Contains(text, "''; DROP") {
		t.Fatal(text, err)
	}
	if err = ValidateSQL(text); err != nil {
		t.Fatal(err)
	}
	if _, err = BindSQL("SELECT :missing", nil); err == nil {
		t.Fatal("missing parameter accepted")
	}
	if _, err = BindSQL("SELECT 1", map[string]any{"unused": 1}); err == nil {
		t.Fatal("unused parameter accepted")
	}
	s := testService(t)
	c, err := s.Import("sql.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SQLWithEngine(context.Background(), []string{c.ID}, "EXPLAIN QUERY PLAN SELECT * FROM c_"+c.ID, "native")
	if err != nil || len(result.Rows) == 0 {
		t.Fatal(result, err)
	}
	if err = ValidateSQL("EXPLAIN DELETE FROM anything"); err == nil {
		t.Fatal("write plan accepted")
	}
}
