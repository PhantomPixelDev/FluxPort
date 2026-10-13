package web

import (
	"encoding/json"
	"testing"
)

func TestCleanupPreviewKeepsEmptyColumnsAndActualValueTypes(t *testing.T) {
	headers, cells, _ := cleanupPreviewRows([]any{map[string]any{"id": json.Number("9007199254740993"), "text": "001", "empty": "", "null": nil}, map[string]any{"id": json.Number("2"), "text": "2", "empty": ""}})
	index := map[string]int{}
	for i, name := range headers {
		index[name] = i
	}
	if _, ok := index["empty"]; !ok {
		t.Fatal("all-empty column disappeared")
	}
	if cell := cells[0][index["id"]]; cell.Text != "9007199254740993" || cell.Kind != "number" {
		t.Fatal("large integer changed", cell)
	}
	if cells[0][index["text"]].Kind != "text" || cells[0][index["empty"]].Kind != "empty" || cells[0][index["null"]].Kind != "null" || cells[1][index["null"]].Kind != "missing" {
		t.Fatal("value distinctions lost", cells)
	}
}
