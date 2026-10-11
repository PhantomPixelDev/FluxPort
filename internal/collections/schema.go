package collections

import (
	"sort"
	"strings"
)

func sortedFields(fields map[string]map[string]bool) []Field {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Field, 0, len(keys))
	for _, k := range keys {
		types := []string{}
		for t := range fields[k] {
			types = append(types, t)
		}
		sort.Strings(types)
		out = append(out, Field{k, strings.Join(types, ", ")})
	}
	return out
}
