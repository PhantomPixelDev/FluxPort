// Package quality provides deterministic, bounded checks without changing input values.
package quality

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Rule struct {
	Field      string       `json:"field"`
	Required   bool         `json:"required,omitempty"`
	NonNull    bool         `json:"non_null,omitempty"`
	NonEmpty   bool         `json:"non_empty,omitempty"`
	Type       string       `json:"type,omitempty"`
	Min        *json.Number `json:"min,omitempty"`
	Max        *json.Number `json:"max,omitempty"`
	MinLength  *int         `json:"min_length,omitempty"`
	MaxLength  *int         `json:"max_length,omitempty"`
	Pattern    string       `json:"pattern,omitempty"`
	Enum       []any        `json:"enum,omitempty"`
	DateLayout string       `json:"date_layout,omitempty"`
	Unique     bool         `json:"unique,omitempty"`
}
type Issue struct {
	Row        int64  `json:"row"`
	Field      string `json:"field"`
	Check      string `json:"check"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Suggestion string `json:"suggestion"`
}
type Validation struct {
	Records         int64   `json:"records_checked"`
	Invalid         int64   `json:"invalid_records"`
	Errors          int64   `json:"error_count"`
	Issues          []Issue `json:"issues"`
	IssuesTruncated bool    `json:"issues_truncated"`
}
type compiled struct {
	Rule
	pattern *regexp.Regexp
	seen    map[[32]byte]bool
}
type Validator struct {
	contract *Contract
	rules    []compiled
	Report   Validation
}

func Number(v any) (*big.Rat, bool) {
	var text string
	switch n := v.(type) {
	case json.Number:
		text = n.String()
	case float64:
		text = fmt.Sprint(n)
	case int:
		text = fmt.Sprint(n)
	case int64:
		text = fmt.Sprint(n)
	default:
		return nil, false
	}
	// A short exponent can otherwise request gigabytes of big-number memory.
	if len(text) > 1024 {
		return nil, false
	}
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(text[i+1:])
		if err != nil || exponent > 1024 || exponent < -1024 {
			return nil, false
		}
	}
	n, ok := new(big.Rat).SetString(text)
	return n, ok
}
func Type(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, int, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// Lookup accepts dotted object paths or JSON pointers, including array indexes.
func Lookup(v any, field string) (any, bool) {
	parts := strings.Split(field, ".")
	if strings.HasPrefix(field, "/") {
		parts = strings.Split(field[1:], "/")
		for i := range parts {
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(parts[i], "~1", "/"), "~0", "~")
		}
	}
	for _, part := range parts {
		switch obj := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = obj[part]
			if !ok {
				return nil, false
			}
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "%d", &index); err != nil || fmt.Sprint(index) != part || index < 0 || index >= len(obj) {
				return nil, false
			}
			v = obj[index]
		default:
			return nil, false
		}
	}
	return v, true
}
func NewValidator(rules []Rule) (*Validator, error) {
	if len(rules) > 64 {
		return nil, fmt.Errorf("validation supports at most 64 rules")
	}
	v := &Validator{Report: Validation{Issues: []Issue{}}}
	for _, r := range rules {
		if r.Field == "" || len(r.Field) > 256 {
			return nil, fmt.Errorf("a field path of 1–256 bytes is required")
		}
		switch r.Type {
		case "", "string", "number", "integer", "boolean", "object", "array", "null":
		default:
			return nil, fmt.Errorf("unsupported type %q", r.Type)
		}
		if len(r.Pattern) > 1024 || len(r.DateLayout) > 128 || len(r.Enum) > 100 {
			return nil, fmt.Errorf("rule exceeds pattern, date, or enum limits")
		}
		for _, n := range []*json.Number{r.Min, r.Max} {
			if n != nil {
				if len(n.String()) > 128 {
					return nil, fmt.Errorf("numeric bound too long")
				}
				if _, ok := Number(*n); !ok {
					return nil, fmt.Errorf("invalid numeric bound")
				}
			}
		}
		if r.Min != nil && r.Max != nil {
			a, _ := Number(*r.Min)
			b, _ := Number(*r.Max)
			if a.Cmp(b) > 0 {
				return nil, fmt.Errorf("minimum exceeds maximum")
			}
		}
		if r.MinLength != nil && *r.MinLength < 0 || r.MaxLength != nil && *r.MaxLength < 0 || r.MinLength != nil && r.MaxLength != nil && *r.MinLength > *r.MaxLength {
			return nil, fmt.Errorf("invalid length bounds")
		}
		c := compiled{Rule: r}
		if r.Pattern != "" {
			var err error
			c.pattern, err = regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", r.Field, err)
			}
		}
		if r.Unique {
			c.seen = map[[32]byte]bool{}
		}
		v.rules = append(v.rules, c)
	}
	return v, nil
}
func (v *Validator) Check(row any) error {
	v.Report.Records++
	before := v.Report.Errors
	v.checkContract(row)
	for i := range v.rules {
		r := &v.rules[i]
		value, exists := Lookup(row, r.Field)
		actual := "missing"
		if exists {
			b, _ := json.Marshal(value)
			actual = string(b)
			if len(actual) > 160 {
				actual = actual[:160] + "…"
			}
		}
		issue := func(check, expected string) {
			v.Report.Errors++
			if len(v.Report.Issues) < 200 {
				v.Report.Issues = append(v.Report.Issues, Issue{v.Report.Records, r.Field, check, expected, actual, "Correct the source value or adjust this rule."})
			} else {
				v.Report.IssuesTruncated = true
			}
		}
		if !exists {
			if r.Required {
				issue("required", "field present")
			}
			continue
		}
		if value == nil {
			if r.NonNull {
				issue("non_null", "non-null value")
			}
			if r.Type != "" && r.Type != "null" {
				issue("type", r.Type)
			}
			continue
		}
		kind := Type(value)
		if r.Type != "" {
			valid := kind == r.Type
			if r.Type == "integer" {
				n, ok := Number(value)
				valid = ok && n.IsInt()
			}
			if !valid {
				issue("type", r.Type)
			}
		}
		if r.Min != nil || r.Max != nil {
			n, ok := Number(value)
			if !ok {
				issue("numeric_range", "number")
			} else {
				if r.Min != nil {
					m, _ := Number(*r.Min)
					if n.Cmp(m) < 0 {
						issue("min", r.Min.String())
					}
				}
				if r.Max != nil {
					m, _ := Number(*r.Max)
					if n.Cmp(m) > 0 {
						issue("max", r.Max.String())
					}
				}
			}
		}
		text, isText := value.(string)
		if r.NonEmpty && (isText && strings.TrimSpace(text) == "" || kind == "array" && len(value.([]any)) == 0 || kind == "object" && len(value.(map[string]any)) == 0) {
			issue("non_empty", "non-empty value")
		}
		if r.MinLength != nil || r.MaxLength != nil {
			if !isText {
				issue("length", "string")
			} else {
				n := utf8.RuneCountInString(text)
				if r.MinLength != nil && n < *r.MinLength {
					issue("min_length", fmt.Sprint(*r.MinLength))
				}
				if r.MaxLength != nil && n > *r.MaxLength {
					issue("max_length", fmt.Sprint(*r.MaxLength))
				}
			}
		}
		if r.pattern != nil && (!isText || !r.pattern.MatchString(text)) {
			issue("pattern", r.Pattern)
		}
		if r.DateLayout != "" {
			var err error
			if isText {
				_, err = time.Parse(r.DateLayout, text)
			}
			if !isText || err != nil {
				issue("date_layout", r.DateLayout)
			}
		}
		if len(r.Enum) > 0 {
			b, _ := json.Marshal(value)
			found := false
			for _, option := range r.Enum {
				o, _ := json.Marshal(option)
				if string(o) == string(b) {
					found = true
					break
				}
			}
			if !found {
				issue("enum", "one of the configured values")
			}
		}
		if r.Unique {
			b, _ := json.Marshal(value)
			key := sha256.Sum256(b)
			if r.seen[key] {
				issue("unique", "distinct value within checked records")
			} else {
				if len(r.seen) >= 10000 {
					return fmt.Errorf("uniqueness check for %s exceeds 10000 distinct values; reduce the scan limit", r.Field)
				}
				r.seen[key] = true
			}
		}
	}
	if v.Report.Errors > before {
		v.Report.Invalid++
	}
	return nil
}
