package collections

import (
	"context"
	"encoding/json"
	"fluxport/internal/conversion"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConversionJobOptionsPersist(t *testing.T) {
	s := testService(t)
	path, e := s.StageConversion("types.csv", strings.NewReader("id;amount;active;formula\n00123;2.50;true;=SUM(1)\n"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	for _, format := range []string{"json", "csv"} {
		j, e := s.ConvertJobWithOptions(path, "csv", format, conversion.Options{CSVDelimiter: "auto", CSVInferTypes: true, SpreadsheetSafe: true})
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
		if j.State != "ready" {
			t.Fatal(j.Error)
		}
		file, e := s.Download(j.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		v, e := conversion.Decode(b, format)
		if e != nil {
			t.Fatal(e)
		}
		row := conversion.Rows(v)[0].(map[string]any)
		if format == "json" && (row["id"] != "00123" || row["amount"] != json.Number("2.50") || row["active"] != true) {
			t.Fatal(row)
		}
		if format == "csv" && row["formula"] != "'=SUM(1)" {
			t.Fatal(row)
		}
	}
}

func TestExportJobsAndHiddenConversionStaging(t *testing.T) {
	s := testService(t)
	c, e := s.Import("users.json", strings.NewReader(`[{"id":1,"nested":{"city":"東京"}},{"id":2,"nested":{"city":"Berlin"}}]`))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Start(context.Background(), 2); e != nil {
		t.Fatal(e)
	}
	defer s.Stop()
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	waitCollection(t, s, c.ID, "ready")
	for _, format := range conversion.Formats {
		j, e := s.ExportJob(c.ID, format)
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
		if j.State != "ready" {
			t.Fatal(format, j.State, j.Error)
		}
		path, e := s.Download(j.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = conversion.Decode(b, format); e != nil {
			t.Fatalf("%s export invalid: %v", format, e)
		}
	}
	path, e := s.StageConversion("convert.ndjson", strings.NewReader("{\"id\":1}\n"))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Sync(); e != nil {
		t.Fatal(e)
	}
	_, n, e := s.List("", 1, 20)
	if e != nil || n != 1 {
		t.Fatal("conversion staging discovered", n, e)
	}
	if _, e = s.ConvertJob(path, "ndjson", "json"); e != nil {
		t.Fatal(e)
	}
}

func TestFilteredExportIncludesAllPagesAndSortOrder(t *testing.T) {
	s := testService(t)
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":1,"active":true},{"id":2,"active":false},{"id":3,"active":true},{"id":4,"active":true}]`))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	if err = s.Sync(); err != nil {
		t.Fatal(err)
	}
	waitCollection(t, s, c.ID, "ready")
	c, err = s.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	q := Query{Filters: []Filter{{Field: "active", Op: "eq", Value: true}}, Sort: []Sort{{Field: "id", Desc: true}}, Page: 2, Limit: 1}
	for _, format := range conversion.Formats {
		j, e := s.ExportQueryJob(c.ID, c.Generation, format, q)
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
		if j.State != "ready" || j.Records != 3 {
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
			rows := v.([]any)
			if len(rows) != 3 || rows[0].(map[string]any)["id"] != json.Number("4") || rows[2].(map[string]any)["id"] != json.Number("1") {
				t.Fatal("pagination leaked into full export", v)
			}
		}
	}
}
