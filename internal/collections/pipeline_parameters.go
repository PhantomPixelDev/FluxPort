package collections

import (
	"bytes"
	"encoding/json"
	"fmt"

	"fluxport/internal/conversion"
)

// BindPipeline replaces explicit {"$parameter":"name"} value nodes only.
// SQL text, identifiers and string contents are never interpolated.
func BindPipeline(p Pipeline, overrides map[string]any) (Pipeline, error) {
	if len(p.Parameters) > 32 || len(overrides) > 32 {
		return p, fmt.Errorf("recipes support at most 32 parameters")
	}
	values := map[string]any{}
	for k, v := range p.Parameters {
		values[k] = v
	}
	for k, v := range overrides {
		if _, ok := values[k]; !ok {
			return p, fmt.Errorf("unknown recipe parameter %q", k)
		}
		values[k] = v
	}
	for k, v := range values {
		if k == "" || len(k) > 64 {
			return p, fmt.Errorf("parameter names need 1–64 bytes")
		}
		b, e := conversion.MarshalJSON(v)
		if e != nil || len(b) > 4096 {
			return p, fmt.Errorf("parameter %q exceeds 4 KiB or is not JSON", k)
		}
	}
	b, err := conversion.MarshalJSON(p.Steps)
	if err != nil {
		return p, err
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&tree); err != nil {
		return p, err
	}
	var walk func(any, int) (any, error)
	walk = func(v any, depth int) (any, error) {
		if depth > 64 {
			return nil, fmt.Errorf("parameter nesting exceeds 64 levels")
		}
		switch x := v.(type) {
		case map[string]any:
			if name, exists := x["$parameter"]; exists {
				if len(x) != 1 {
					return nil, fmt.Errorf("parameter nodes cannot have other properties")
				}
				key, ok := name.(string)
				if !ok {
					return nil, fmt.Errorf("parameter name must be text")
				}
				value, ok := values[key]
				if !ok {
					return nil, fmt.Errorf("missing recipe parameter %q", key)
				}
				return value, nil
			}
			for k, value := range x {
				next, e := walk(value, depth+1)
				if e != nil {
					return nil, e
				}
				x[k] = next
			}
		case []any:
			for i, value := range x {
				next, e := walk(value, depth+1)
				if e != nil {
					return nil, e
				}
				x[i] = next
			}
		}
		return v, nil
	}
	tree, err = walk(tree, 0)
	if err != nil {
		return p, err
	}
	b, err = conversion.MarshalJSON(tree)
	if err != nil {
		return p, err
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&p.Steps); err != nil {
		return p, err
	}
	p.Parameters = nil
	return p, nil
}
