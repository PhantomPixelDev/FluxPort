package conversion

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ParseColumnTypes accepts one name=type per line, with literal CSV header names.
func ParseColumnTypes(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, kind, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		kind = strings.TrimSpace(kind)
		if !ok || key == "" {
			return nil, fmt.Errorf("CSV column rules must use name=type, one per line")
		}
		switch kind {
		case "text", "number", "boolean", "null", "json", "auto":
		default:
			return nil, fmt.Errorf("CSV column %q: choose text, number, boolean, null, json or auto", key)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate CSV column rule %q", key)
		}
		out[key] = kind
	}
	return out, nil
}

func csvColumn(s, key string, o Options) (any, error) {
	kind := o.CSVColumnTypes[key]
	switch kind {
	case "text":
		return s, nil
	case "number":
		var n json.Number
		if err := json.Unmarshal([]byte(s), &n); err != nil || s == "" || n.String() == "" || strings.TrimSpace(s) != s || s[0] == '"' {
			return nil, fmt.Errorf("expected a JSON-style number; use text for identifiers or leading zeros")
		}
		return n, nil
	case "boolean":
		switch strings.ToLower(s) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("expected true or false")
	case "null":
		if s == "" || strings.EqualFold(s, "null") {
			return nil, nil
		}
		return nil, fmt.Errorf("expected an empty cell or null")
	case "json":
		return decodeJSON([]byte(s))
	case "", "auto":
	default:
		return nil, fmt.Errorf("unsupported column type %q", kind)
	}
	if o.CSVParseJSON && (strings.HasPrefix(strings.TrimSpace(s), "{") || strings.HasPrefix(strings.TrimSpace(s), "[")) {
		value, err := decodeJSON([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("invalid nested JSON: %w", err)
		}
		return value, nil
	}
	return csvScalar(s, o.CSVInferTypes || kind == "auto"), nil
}

type TreeNode struct {
	Label, Value string
	Children     []TreeNode
}

func validateColumnRules(headers []string, o Options) error {
	known := map[string]bool{}
	for _, h := range headers {
		known[h] = true
	}
	for h, kind := range o.CSVColumnTypes {
		if !known[h] {
			return fmt.Errorf("CSV column rule %q does not match a header", h)
		}
		switch kind {
		case "text", "number", "boolean", "null", "json", "auto":
		default:
			return fmt.Errorf("CSV column %q has unsupported type %q", h, kind)
		}
	}
	return nil
}

func ValueNotes(v any, to string, o Options) []string {
	if Format(to) != "csv" {
		return nil
	}
	rows := Rows(v)
	limit := len(rows)
	if limit > 1000 {
		limit = 1000
	}
	nested, nulls, safe, missing := 0, 0, 0, 0
	keys := map[string]bool{}
	for _, r := range rows[:limit] {
		if m, ok := r.(map[string]any); ok {
			for k := range m {
				keys[k] = true
			}
		}
	}
	for _, r := range rows[:limit] {
		if m, ok := r.(map[string]any); ok {
			missing += len(keys) - len(m)
			for _, value := range m {
				switch x := value.(type) {
				case map[string]any, []any:
					nested++
				case nil:
					nulls++
				case string:
					if o.SpreadsheetSafe && spreadsheetCell(x) != x {
						safe++
					}
				}
			}
		}
	}
	notes := []string{fmt.Sprintf("Inspected %d of %d records: %d nested cells become JSON text; %d null and %d missing cells become empty text; %d formula-like text cells are prefixed.", limit, len(rows), nested, nulls, missing, safe)}
	if limit < len(rows) {
		notes = append(notes, "Cell counts are a sample of the first 1,000 records.")
	}
	return notes
}

// Preview is bounded and uses exact number text, never JavaScript float coercion.
func TreePreview(v any) []TreeNode {
	budget := 500
	var node func(string, any, int) TreeNode
	node = func(label string, v any, depth int) TreeNode {
		n := TreeNode{Label: label}
		budget--
		if budget <= 0 || depth >= 10 {
			n.Value = "Preview limit reached"
			return n
		}
		switch x := v.(type) {
		case map[string]any:
			n.Value = fmt.Sprintf("object · %d fields", len(x))
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if budget <= 0 {
					n.Children = append(n.Children, TreeNode{Value: "More fields omitted"})
					break
				}
				n.Children = append(n.Children, node(k, x[k], depth+1))
			}
		case []any:
			n.Value = fmt.Sprintf("array · %d items", len(x))
			for i, a := range x {
				if budget <= 0 {
					n.Children = append(n.Children, TreeNode{Value: "More items omitted"})
					break
				}
				n.Children = append(n.Children, node(fmt.Sprintf("[%d]", i), a, depth+1))
			}
		default:
			b, err := MarshalJSON(v)
			if err != nil {
				n.Value = fmt.Sprint(v)
			} else {
				n.Value = string(b)
			}
			if len(n.Value) > 500 {
				n.Value = n.Value[:500] + "…"
			}
		}
		return n
	}
	return []TreeNode{node("$", v, 0)}
}

// Differences compares normalized values and reports bounded field paths.
func Differences(a, b any) []string {
	out := []string{}
	budget := 10000
	var walk func(any, any, string)
	walk = func(a, b any, p string) {
		if len(out) >= 50 || budget <= 0 {
			return
		}
		budget--
		am, aok := a.(map[string]any)
		bm, bok := b.(map[string]any)
		if aok && bok {
			keys := map[string]bool{}
			for k := range am {
				keys[k] = true
			}
			for k := range bm {
				keys[k] = true
			}
			names := []string{}
			for k := range keys {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				av, ae := am[k]
				bv, be := bm[k]
				q := p + "[" + fmt.Sprintf("%q", k) + "]"
				if ae != be {
					if len(out) < 50 {
						out = append(out, q+": field added or removed")
					}
				} else {
					walk(av, bv, q)
				}
			}
			return
		}
		aa, aok := a.([]any)
		ba, bok := b.([]any)
		if aok && bok {
			if len(aa) != len(ba) {
				out = append(out, p+": array length changed")
			}
			for i := 0; i < len(aa) && i < len(ba) && budget > 0; i++ {
				walk(aa[i], ba[i], fmt.Sprintf("%s[%d]", p, i))
			}
			return
		}
		if !reflect.DeepEqual(a, b) {
			out = append(out, p+": value or type changed")
		}
	}
	walk(a, b, "$")
	if budget <= 0 || len(out) >= 50 {
		out = append(out, "Comparison limit reached; remaining values were not checked")
	}
	return out
}

func ExplainError(input []byte, err error, format string) string {
	message := err.Error()
	line, column := 0, 0
	var js *json.SyntaxError
	var ce *csv.ParseError
	var xe *xml.SyntaxError
	switch {
	case errors.As(err, &js):
		offset := int(js.Offset) - 1
		if offset < 0 {
			offset = 0
		}
		if offset > len(input) {
			offset = len(input)
		}
		line = 1 + strings.Count(string(input[:offset]), "\n")
		last := strings.LastIndex(string(input[:offset]), "\n")
		column = offset - last
	case errors.As(err, &ce):
		line, column = ce.Line, ce.Column
	case errors.As(err, &xe):
		line = xe.Line
	}
	if line > 0 {
		lines := strings.Split(string(input), "\n")
		if line <= len(lines) {
			snippet := lines[line-1]
			if len(snippet) > 160 {
				snippet = snippet[:160] + "…"
			}
			message = fmt.Sprintf("Line %d, column %d: %s\n%s", line, column, message, snippet)
		}
	}
	hint := "Check the highlighted value and the selected input format."
	switch Format(format) {
	case "json", "ndjson":
		hint = "Use double-quoted keys and strings; remove trailing commas and duplicate keys."
	case "csv":
		hint = "Check the delimiter, quoted multiline fields, unique headers and column type rules."
	case "yaml":
		hint = "Check indentation, aliases and scalar types."
	case "toml":
		hint = "Check table names and numeric limits; TOML cannot represent null."
	case "xml":
		hint = "Check matching tags, escaped text and unique attributes."
	}
	return message + "\n" + hint
}
