package transformations

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/quality"
)

// Operation targets literal top-level column names; nested structures can first
// be flattened. It is shared by pipelines and the native processing path.
type Operation struct {
	Kind         string   `json:"kind"`
	Field        string   `json:"field,omitempty"`
	As           string   `json:"as,omitempty"`
	Value        any      `json:"value,omitempty"`
	Find         string   `json:"find,omitempty"`
	Replacement  string   `json:"replacement,omitempty"`
	Start        int      `json:"start,omitempty"`
	Length       int      `json:"length,omitempty"`
	Delimiter    string   `json:"delimiter,omitempty"`
	Fields       []string `json:"fields,omitempty"`
	Layout       string   `json:"layout,omitempty"`
	OutputLayout string   `json:"output_layout,omitempty"`
}

func (o Operation) Validate() error {
	switch o.Kind {
	case "trim", "lower", "upper", "replace", "regex", "substring", "split", "merge", "default", "cast_string", "cast_number", "cast_boolean", "add", "subtract", "multiply", "date", "flatten":
	default:
		return fmt.Errorf("unsupported transform %q", o.Kind)
	}
	if o.Kind != "flatten" && o.Kind != "merge" && (o.Field == "" || len(o.Field) > 256) {
		return fmt.Errorf("transform requires a field of 1–256 bytes")
	}
	if len(o.As) > 256 || len(o.Find) > 1024 || len(o.Replacement) > 4096 || len(o.Delimiter) > 128 || len(o.Fields) > 32 || len(o.Layout) > 128 || len(o.OutputLayout) > 128 {
		return fmt.Errorf("transform configuration exceeds limits")
	}
	if o.Start < 0 || o.Length < 0 {
		return fmt.Errorf("substring offsets must be non-negative")
	}
	if o.Kind == "regex" {
		r, err := regexp.Compile(o.Find)
		if err != nil {
			return err
		}
		if r.NumSubexp() > 32 {
			return fmt.Errorf("regex supports at most 32 capture groups")
		}
	}
	if o.Kind == "merge" && (o.As == "" || len(o.Fields) == 0) {
		return fmt.Errorf("merge requires fields and a target column")
	}
	if o.Kind == "split" && o.Delimiter == "" {
		return fmt.Errorf("split requires a non-empty delimiter")
	}
	if o.Kind == "date" && (o.Layout == "" || o.OutputLayout == "") {
		return fmt.Errorf("date requires input and output layouts")
	}
	return nil
}
func ApplyOperation(row any, o Operation) (any, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	obj, ok := row.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("transform requires an object record")
	}
	out := map[string]any{}
	for key, value := range obj {
		out[key] = value
	}
	if o.Kind == "flatten" {
		out = map[string]any{}
		var walk func(map[string]any, string, int) error
		walk = func(m map[string]any, prefix string, depth int) error {
			if depth > 32 {
				return fmt.Errorf("flatten exceeds 32 nesting levels")
			}
			keys := []string{}
			for key := range m {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				name := prefix + key
				value := m[key]
				if nested, ok := value.(map[string]any); ok && len(nested) > 0 {
					if err := walk(nested, name+".", depth+1); err != nil {
						return err
					}
				} else {
					if _, exists := out[name]; exists {
						return fmt.Errorf("flatten creates duplicate column %q", name)
					}
					if len(out) >= 2048 {
						return fmt.Errorf("flatten exceeds 2048 columns")
					}
					out[name] = value
				}
			}
			return nil
		}
		if err := walk(obj, "", 0); err != nil {
			return nil, err
		}
		return out, nil
	}
	target := o.As
	if target == "" {
		target = o.Field
	}
	value, exists := obj[o.Field]
	if o.Kind == "default" {
		if !exists || value == nil || value == "" {
			out[target] = o.Value
		} else {
			out[target] = value
		}
		return out, nil
	}
	if o.Kind == "merge" {
		parts := []string{}
		for _, field := range o.Fields {
			v, ok := obj[field].(string)
			if !ok {
				return nil, fmt.Errorf("merge column %q must be text", field)
			}
			parts = append(parts, v)
		}
		out[target] = strings.Join(parts, o.Delimiter)
		return out, nil
	}
	if !exists {
		return nil, fmt.Errorf("column %q is missing", o.Field)
	}
	text, isText := value.(string)
	switch o.Kind {
	case "cast_string":
		if isText {
			out[target] = text
		} else {
			b, err := conversion.MarshalJSON(value)
			if err != nil {
				return nil, err
			}
			out[target] = string(b)
		}
	case "cast_number":
		if !isText {
			if _, ok := value.(json.Number); !ok {
				return nil, fmt.Errorf("cast_number requires text or a number")
			}
			out[target] = value
		} else {
			v, err := conversion.Decode([]byte(text), "json")
			if err != nil {
				return nil, fmt.Errorf("column %q is not a JSON number", o.Field)
			}
			if _, ok := v.(json.Number); !ok {
				return nil, fmt.Errorf("column %q is not a number", o.Field)
			}
			out[target] = v
		}
	case "cast_boolean":
		if b, ok := value.(bool); ok {
			out[target] = b
		} else if isText && (text == "true" || text == "false") {
			out[target] = text == "true"
		} else {
			return nil, fmt.Errorf("cast_boolean requires true or false")
		}
	case "add", "subtract", "multiply":
		a, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("numeric transform requires a number")
		}
		b, ok := o.Value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("numeric operand must be a JSON number")
		}
		x, ok := quality.Number(a)
		if !ok {
			return nil, fmt.Errorf("numeric source exceeds 1024 digits or exponent range")
		}
		y, ok := quality.Number(b)
		if !ok {
			return nil, fmt.Errorf("numeric operand exceeds 1024 digits or exponent range")
		}
		switch o.Kind {
		case "add":
			x.Add(x, y)
		case "subtract":
			x.Sub(x, y)
		case "multiply":
			x.Mul(x, y)
		}
		// Reject results requiring more than 128 decimal places rather than round.
		text := strings.TrimRight(strings.TrimRight(x.FloatString(128), "0"), ".")
		check, ok := quality.Number(json.Number(text))
		if !ok || check.Cmp(x) != 0 {
			return nil, fmt.Errorf("computed number exceeds supported precision (1024 digits, 128 decimal places)")
		}
		out[target] = json.Number(text)
	default:
		if !isText {
			return nil, fmt.Errorf("column %q must be text for %s", o.Field, o.Kind)
		}
		switch o.Kind {
		case "trim":
			out[target] = strings.TrimSpace(text)
		case "lower":
			out[target] = strings.ToLower(text)
		case "upper":
			out[target] = strings.ToUpper(text)
		case "replace":
			if len(o.Replacement) > len(o.Find) && int64(len(text))+int64(strings.Count(text, o.Find))*int64(len(o.Replacement)-len(o.Find)) > 1<<20 {
				return nil, fmt.Errorf("replacement exceeds 1 MiB output limit")
			}
			out[target] = strings.ReplaceAll(text, o.Find, o.Replacement)
		case "regex":
			r, _ := regexp.Compile(o.Find)
			if err := boundedRegex(text, r, o.Replacement); err != nil {
				return nil, err
			}
			out[target] = r.ReplaceAllString(text, o.Replacement)
		case "substring":
			r := []rune(text)
			start := min(o.Start, len(r))
			end := len(r)
			if o.Length > 0 && o.Length < len(r)-start {
				end = start + o.Length
			}
			out[target] = string(r[start:end])
		case "split":
			if strings.Count(text, o.Delimiter) >= 10000 {
				return nil, fmt.Errorf("split exceeds 10000 items")
			}
			pieces := strings.Split(text, o.Delimiter)
			if len(pieces) > 10000 {
				return nil, fmt.Errorf("split exceeds 10000 items")
			}
			values := make([]any, len(pieces))
			for i, p := range pieces {
				values[i] = p
			}
			out[target] = values
		case "date":
			parsed, err := time.Parse(o.Layout, text)
			if err != nil {
				return nil, fmt.Errorf("column %q: %w", o.Field, err)
			}
			out[target] = parsed.Format(o.OutputLayout)
		}
	}
	b, err := conversion.MarshalJSON(out)
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("transformed record exceeds 1 MiB")
	}
	return out, nil
}

var replacementTokens = regexp.MustCompile(`\$\$|\$\{([A-Za-z0-9_]+)\}|\$([A-Za-z0-9_]+)`)

// Calculate expansion size before replacement allocates output. RE2 bounds
// execution time, but does not itself bound amplification from replacements.
func boundedRegex(text string, r *regexp.Regexp, replacement string) error {
	matches := r.FindAllStringSubmatchIndex(text, 10001)
	if len(matches) > 10000 {
		return fmt.Errorf("regex exceeds 10000 matches per record")
	}
	tokens := replacementTokens.FindAllStringSubmatchIndex(replacement, -1)
	size := int64(len(text))
	for _, match := range matches {
		expanded := int64(len(replacement))
		for _, token := range tokens {
			literal := replacement[token[0]:token[1]]
			if literal == "$$" {
				expanded--
				continue
			}
			start, end := token[2], token[3]
			if start < 0 {
				start, end = token[4], token[5]
			}
			name := replacement[start:end]
			group, e := strconv.Atoi(name)
			if e != nil {
				group = r.SubexpIndex(name)
			}
			capture := 0
			if group >= 0 && group < len(match)/2 && match[group*2] >= 0 {
				capture = match[group*2+1] - match[group*2]
			}
			expanded += int64(capture - len(literal))
		}
		size += expanded - int64(match[1]-match[0])
	}
	if size > 1<<20 {
		return fmt.Errorf("regex replacement exceeds 1 MiB output limit")
	}
	return nil
}
