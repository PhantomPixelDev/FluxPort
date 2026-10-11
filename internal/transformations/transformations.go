package transformations

import (
	"encoding/json"
	"fmt"
)

// Rules are deliberately declarative: no arbitrary code is evaluated.
type Rules struct {
	Select []string          `json:"select,omitempty"`
	Drop   []string          `json:"drop,omitempty"`
	Rename map[string]string `json:"rename,omitempty"`
	Filter map[string]any    `json:"filter,omitempty"`
}

func Parse(s string) (Rules, error) {
	var r Rules
	if s == "" {
		return r, nil
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, err
	}
	for from, to := range r.Rename {
		if from == "" || to == "" {
			return r, fmt.Errorf("rename fields cannot be empty")
		}
	}
	return r, nil
}
func Apply(rows []any, r Rules) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			if len(r.Filter) == 0 {
				out = append(out, row)
			}
			continue
		}
		match := true
		for k, want := range r.Filter {
			a, _ := json.Marshal(obj[k])
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		next := map[string]any{}
		if len(r.Select) > 0 {
			for _, k := range r.Select {
				if v, ok := obj[k]; ok {
					next[k] = v
				}
			}
		} else {
			for k, v := range obj {
				next[k] = v
			}
		}
		for _, k := range r.Drop {
			delete(next, k)
		}
		// Resolve renames against an immutable source so iteration order is irrelevant.
		original := map[string]any{}
		for k, v := range next {
			original[k] = v
		}
		for from := range r.Rename {
			delete(next, from)
		}
		for from, to := range r.Rename {
			if v, ok := original[from]; ok {
				next[to] = v
			}
		}
		out = append(out, next)
	}
	return out
}
