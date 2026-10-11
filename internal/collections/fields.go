package collections

import "encoding/json"

type Field struct {
	Name  string
	Types string
}

func Schema(rows []any) []Field {
	fields := map[string]map[string]bool{}
	for _, row := range rows {
		if obj, ok := row.(map[string]any); ok {
			for k, v := range obj {
				if fields[k] == nil {
					fields[k] = map[string]bool{}
				}
				t := "null"
				switch v.(type) {
				case string:
					t = "string"
				case bool:
					t = "boolean"
				case json.Number, float64:
					t = "number"
				case []any:
					t = "array"
				case map[string]any:
					t = "object"
				}
				fields[k][t] = true
			}
		}
	}
	// Sorting is performed below to keep schema stable across refreshes.
	return sortedFields(fields)
}
