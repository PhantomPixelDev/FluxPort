package conversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// JSON decoding must not overwrite duplicate fields or replace invalid Unicode.
func decodeJSON(b []byte) (any, error) {
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("input contains invalid UTF-8")
	}
	if e := validSurrogates(b); e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, e := jsonValue(d, "$", 0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, fmt.Errorf("expected a single JSON value")
	}
	return v, nil
}
func jsonValue(d *json.Decoder, path string, depth int) (any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("%s: nesting exceeds 512 levels", path)
	}
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	switch t {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			key := k.(string)
			if _, ok := m[key]; ok {
				return nil, fmt.Errorf("%s: duplicate JSON key %q", path, key)
			}
			v, e := jsonValue(d, path+"."+key, depth+1)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		_, e = d.Token()
		return m, e
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, e := jsonValue(d, fmt.Sprintf("%s[%d]", path, len(a)), depth+1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, e = d.Token()
		return a, e
	default:
		return t, nil
	}
}
func validSurrogates(b []byte) error {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) || b[i] != 'u' || i+4 >= len(b) {
			continue
		}
		n, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if e != nil {
			continue
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return fmt.Errorf("unpaired Unicode surrogate near byte %d", i)
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return fmt.Errorf("unpaired Unicode surrogate near byte %d", i)
			}
			low, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired Unicode surrogate near byte %d", i)
			}
			i += 6
		}
	}
	return nil
}

func decodeYAML(b []byte) (any, error) {
	d := yaml.NewDecoder(bytes.NewReader(b))
	var n yaml.Node
	if e := d.Decode(&n); e != nil {
		return nil, e
	}
	var extra yaml.Node
	if e := d.Decode(&extra); e != io.EOF {
		return nil, fmt.Errorf("expected a single YAML document")
	}
	budget := 1000000
	return yamlValue(&n, "$", 0, &budget)
}
func yamlValue(n *yaml.Node, path string, depth int, budget *int) (any, error) {
	*budget--
	if depth > 512 || *budget < 0 {
		return nil, fmt.Errorf("%s: YAML nesting or alias expansion limit exceeded", path)
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, errEmptyInput
		}
		return yamlValue(n.Content[0], path, depth+1, budget)
	case yaml.AliasNode:
		if n.Alias == nil {
			return nil, fmt.Errorf("%s: invalid YAML alias", path)
		}
		return yamlValue(n.Alias, path, depth+1, budget)
	case yaml.SequenceNode:
		a := make([]any, 0, len(n.Content))
		for i, c := range n.Content {
			v, e := yamlValue(c, fmt.Sprintf("%s[%d]", path, i), depth+1, budget)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, nil
	case yaml.MappingNode:
		m := map[string]any{}
		seen := map[string]bool{}
		// Merge defaults first; explicitly declared keys always override defaults.
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Tag != "!!merge" {
				continue
			}
			v, e := yamlValue(n.Content[i+1], path+".<<", depth+1, budget)
			if e != nil {
				return nil, e
			}
			values := []any{v}
			if a, ok := v.([]any); ok {
				values = a
			}
			for _, v := range values {
				defaults, ok := v.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%s: YAML merge requires mappings", path)
				}
				for k, x := range defaults {
					if _, ok := m[k]; !ok {
						m[k] = x
					}
				}
			}
		}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag == "!!merge" {
				if seen["<<"] {
					return nil, fmt.Errorf("%s: duplicate YAML merge key", path)
				}
				seen["<<"] = true
				continue
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, fmt.Errorf("%s: YAML object keys must be strings", path)
			}
			if seen[k.Value] {
				return nil, fmt.Errorf("%s: duplicate YAML key %q", path, k.Value)
			}
			seen[k.Value] = true
			v, e := yamlValue(n.Content[i+1], path+"."+k.Value, depth+1, budget)
			if e != nil {
				return nil, e
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str", "!!timestamp":
			return n.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			var v bool
			e := n.Decode(&v)
			return v, e
		case "!!int":
			s := strings.ReplaceAll(n.Value, "_", "")
			if json.Valid([]byte(s)) {
				return json.Number(s), nil
			}
			v, ok := new(big.Int).SetString(s, 0)
			if !ok {
				return nil, fmt.Errorf("%s: invalid YAML integer", path)
			}
			return json.Number(v.String()), nil
		case "!!float":
			s := strings.ReplaceAll(n.Value, "_", "")
			s = strings.TrimPrefix(s, "+")
			if json.Valid([]byte(s)) {
				return json.Number(s), nil
			}
			if strings.HasPrefix(s, ".") {
				s = "0" + s
			}
			if strings.HasPrefix(s, "-.") {
				s = "-0" + s[1:]
			}
			if i := strings.IndexByte(s, '.'); i >= 0 && (i == len(s)-1 || s[i+1] == 'e' || s[i+1] == 'E') {
				s = s[:i+1] + "0" + s[i+1:]
			}
			parts := strings.SplitN(strings.ToLower(s), "e", 2)
			mantissa := parts[0]
			sign := ""
			if strings.HasPrefix(mantissa, "-") {
				sign = "-"
				mantissa = mantissa[1:]
			}
			digits := strings.SplitN(mantissa, ".", 2)
			digits[0] = strings.TrimLeft(digits[0], "0")
			if digits[0] == "" {
				digits[0] = "0"
			}
			s = sign + strings.Join(digits, ".")
			if len(parts) == 2 {
				s += "e" + parts[1]
			}
			if !json.Valid([]byte(s)) {
				return nil, fmt.Errorf("%s: YAML number is not a finite JSON-compatible number", path)
			}
			return json.Number(s), nil
		default:
			return nil, fmt.Errorf("%s: YAML tag %q cannot be represented in JSON", path, n.Tag)
		}
	}
	return nil, fmt.Errorf("%s: unsupported YAML value", path)
}
