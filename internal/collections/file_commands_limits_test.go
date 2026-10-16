package collections

import (
	"os"
	"strings"
	"testing"
)

func TestFileCommandExpansionsAreBoundedAndAtomic(t *testing.T) {
	s := testService(t)
	input := "[" + strings.TrimSuffix(strings.Repeat(`{"name":"a"},`, 100), ",") + "]"
	c, err := s.Import("growth.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	c, _ = fileRows(t, s, c.ID)
	s.MaxBytes = 2048
	for _, command := range []FileCommand{
		{Action: "add_column", Name: "extra", Value: strings.Repeat("x", 512)},
		{Action: "find_replace", Find: "a", Replacement: strings.Repeat("x", 512), Fields: []string{"name"}},
	} {
		command.Generation = c.Generation
		if _, err = s.FileCommand(c.ID, command, true); err == nil {
			t.Fatal("expansion accepted")
		}
		if b, _ := os.ReadFile(c.Path); string(b) != input {
			t.Fatal("partial expansion saved")
		}
	}
}
