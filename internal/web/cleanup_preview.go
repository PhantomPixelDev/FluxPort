package web

import (
	"encoding/json"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
)

type CleanupCell struct{ Text, Kind string }

func cleanupPreviewRows(rows []any) ([]string, [][]CleanupCell, int) {
	fields := collections.Schema(rows)
	available := map[string]bool{}
	for _, field := range fields {
		available[field.Name] = true
	}
	headers := []string{}
	for _, name := range []string{"id", "name", "email", "sku", "status", "event_type", "timestamp"} {
		if available[name] {
			headers = append(headers, name)
			delete(available, name)
		}
	}
	for _, field := range fields {
		if available[field.Name] {
			headers = append(headers, field.Name)
		}
	}
	if len(headers) > 48 {
		headers = headers[:48]
	}
	cells := [][]CleanupCell{}
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			return nil, nil, len(fields)
		}
		line := []CleanupCell{}
		for _, header := range headers {
			value, exists := object[header]
			cell := CleanupCell{Text: "missing", Kind: "missing"}
			if exists {
				cell.Kind = "text"
				switch value := value.(type) {
				case nil:
					cell.Text, cell.Kind = "null", "null"
				case string:
					cell.Text = value
					if value == "" {
						cell.Text, cell.Kind = "empty text", "empty"
					}
				default:
					data, _ := conversion.MarshalJSON(value)
					cell.Text = string(data)
					switch value.(type) {
					case json.Number, int, int64, float64:
						cell.Kind = "number"
					case bool:
						cell.Kind = "boolean"
					default:
						cell.Kind = "json"
					}
				}
			}
			runes := []rune(cell.Text)
			if len(runes) > 512 {
				cell.Text = string(runes[:512]) + "…"
			}
			line = append(line, cell)
		}
		cells = append(cells, line)
	}
	return headers, cells, len(fields)
}
