package collections

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"fluxport/internal/jobs"
	"fluxport/internal/transformations"
)

func runFileCleanup(t *testing.T, s *Service, p Pipeline) jobs.Job {
	t.Helper()
	c, err := s.Get(p.Source)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.FileCleanupJob(p, c.Generation, "Trim matching names")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE jobs SET state='running' WHERE id=?", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.fileCleanupJob(context.Background(), j, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	j, err = s.Jobs.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func TestFileCleanupFilteredAcrossPagesAndUndo(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 1 << 20
	original := "[\n" + strings.Repeat(`{"name":" untouched ","group":"outside","big":18446744073709551617},`+"\n", 30) + strings.Repeat(`{"name":" Ada ","group":"target","big":9007199254740993},`+"\n", 129) + `{"name":" Ada ","group":"target","big":9007199254740993}` + "\n]\n"
	c, err := s.Import("scope.json", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	c, before := fileRows(t, s, c.ID)
	q := Query{Page: 2, Limit: 20, Filters: []Filter{{Field: "/group", Op: "eq", Value: "target"}}, Sort: []Sort{{Field: "/name", Desc: true}}}
	p := Pipeline{Version: 1, Source: c.ID, InputQuery: &q, Steps: []PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}
	job := runFileCleanup(t, s, p)
	if job.State != "ready" || job.Result != "file-saved" {
		t.Fatal("commit was not durable", job)
	}
	c, first := fileRows(t, s, c.ID)
	if c.Rows != 160 || first.Records[0].ID != before.Records[0].ID {
		t.Fatal("scope changed unrelated rows or identity")
	}
	for _, row := range first.Records {
		value := row.Value.(map[string]any)
		if value["group"] == "outside" && value["name"] != " untouched " {
			t.Fatal("outside row changed", value)
		}
		if value["group"] == "target" && value["name"] != "Ada" {
			t.Fatal("matching row not cleaned", value)
		}
	}
	last, err := s.Query(context.Background(), c, Query{Page: 2, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Records) != 60 || last.Records[59].Value.(map[string]any)["name"] != "Ada" {
		t.Fatal("cleanup only used one page")
	}
	if err = s.Jobs.Cancel(job.ID); !errors.Is(err, jobs.ErrFileCommitted) {
		t.Fatal("committed job was cancellable", err)
	}
	if err = s.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(c.Path)
	if string(b) != original {
		t.Fatal("Undo did not restore original bytes")
	}
}
func TestFileCleanupCancellationAndSourceConflict(t *testing.T) {
	s := testService(t)
	c, err := s.Import("cancel.json", strings.NewReader(`[{"name":" Ada "}]`))
	if err != nil {
		t.Fatal(err)
	}
	p := Pipeline{Source: c.ID, Version: 1, Steps: []PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}
	j, err := s.FileCleanupJob(p, c.Generation, "Trim")
	if err != nil {
		t.Fatal(err)
	}
	s.DB.Exec("UPDATE jobs SET state='running' WHERE id=?", j.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.fileCleanupJob(ctx, j, func(int64, int64) {}); err == nil {
		t.Fatal("cancelled cleanup wrote file")
	}
	b, _ := os.ReadFile(c.Path)
	if string(b) != `[{"name":" Ada "}]` {
		t.Fatal(string(b))
	}
	if err = os.WriteFile(c.Path, []byte(`[{"name":"External"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.fileCleanupJob(context.Background(), j, func(int64, int64) {}); err == nil {
		t.Fatal("changed source accepted")
	}
	b, _ = os.ReadFile(c.Path)
	if string(b) != `[{"name":"External"}]` {
		t.Fatal("external source overwritten")
	}
}
func TestFileDeleteCSVRetainsHeadersAndExactUndo(t *testing.T) {
	s := testService(t)
	original := "id,note\r\n001,\"line one\nline two\"\r\n"
	c, err := s.Import("empty.csv", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	c, rows := fileRows(t, s, c.ID)
	if _, err = s.EditFile(c.ID, c.Generation, []Mutation{{ID: rows.Records[0].ID, Method: "DELETE"}}); err != nil {
		t.Fatal(err)
	}
	c, rows = fileRows(t, s, c.ID)
	if c.Rows != 0 || len(rows.Records) != 0 || c.Status != "ready" {
		t.Fatal(c)
	}
	if err = s.RestoreFile(c.ID, c.Generation, "undo", 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(c.Path)
	if string(b) != original {
		t.Fatal("CSV bytes not restored")
	}
}
