package quality

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestSingleColumnProfileKeepsWholeRowDuplicateSemantics(t *testing.T) {
	p := NewProfiler()
	if err := p.EnsureColumn("name"); err != nil {
		t.Fatal(err)
	}
	p.AddColumn(map[string]any{"name": "Ada", "id": 1}, "name")
	p.AddColumn(map[string]any{"name": "Ada", "id": 2}, "name")
	p.AddColumn(map[string]any{"id": 3}, "name")
	columns := p.Columns()
	if p.DuplicateRows != 0 || len(columns) != 1 || columns[0].Missing != 1 || columns[0].Common[0].Count != 2 {
		t.Fatal(p.DuplicateRows, columns)
	}
}

func TestSummaryTracksStatesDuplicatesAndBoundedCommonValues(t *testing.T) {
	p := NewProfiler()
	row := map[string]any{"name": " Ada ", "empty": "", "nil": nil, "big": json.Number("9007199254740993")}
	if err := p.Add(row); err != nil {
		t.Fatal(err)
	}
	p.Add(row)
	p.Add(map[string]any{"name": "Grace"})
	if p.DuplicateRows != 1 {
		t.Fatal(p.DuplicateRows)
	}
	for _, c := range p.Columns() {
		switch c.Name {
		case "name":
			if c.Whitespace != 2 || c.Common[0].Count != 2 || c.Repeated != 1 {
				t.Fatal(c)
			}
		case "empty":
			if c.Empty != 2 || c.Missing != 1 {
				t.Fatal(c)
			}
		case "big":
			if c.Min != "9007199254740993" {
				t.Fatal(c)
			}
		}
	}
	for i := 0; i < 1100; i++ {
		p.Add(map[string]any{"name": fmt.Sprint(i)})
	}
	for _, c := range p.Columns() {
		if c.Name == "name" && (!c.CommonCapped || !c.DistinctCapped || len(c.Common) > 5) {
			t.Fatal("unbounded common values", c)
		}
	}
}
