package collections

import (
	"strings"
	"testing"
)

func TestExplicitNullRowIsNotChangedToEmptyObject(t *testing.T) {
	s := testService(t)
	c, err := s.Import("null-row.json", strings.NewReader("[]"))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	p, err := s.FileCommand(c.ID, FileCommand{Generation: c.Generation, Action: "add_row", Value: nil}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Sample) != 1 || p.Sample[0][0] != "null" {
		t.Fatal("preview altered null", p)
	}
	_, rows := fileRows(t, s, c.ID)
	if len(rows.Records) != 1 || rows.Records[0].Value != nil {
		t.Fatal("null row altered", rows)
	}
}
