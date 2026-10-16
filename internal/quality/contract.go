package quality

import (
	"fmt"
	"sort"
	"strings"
)

// Contract describes literal top-level columns. Types never coerce source values.
type Contract struct {
	Columns    []ContractColumn `json:"columns"`
	AllowExtra bool             `json:"allow_extra"`
}
type ContractColumn struct {
	Name     string   `json:"name"`
	Types    []string `json:"types"`
	Required bool     `json:"required"`
	Nullable bool     `json:"nullable"`
}

func (c Contract) Validate() error {
	if len(c.Columns) == 0 || len(c.Columns) > 256 {
		return fmt.Errorf("a contract needs 1–256 columns")
	}
	seen := map[string]bool{}
	for _, col := range c.Columns {
		if col.Name == "" || len(col.Name) > 256 || seen[col.Name] {
			return fmt.Errorf("contract column names must be unique and 1–256 bytes")
		}
		seen[col.Name] = true
		if len(col.Types) == 0 || len(col.Types) > 7 {
			return fmt.Errorf("choose expected types for %s", col.Name)
		}
		for _, typ := range col.Types {
			switch typ {
			case "string", "number", "integer", "boolean", "object", "array", "null":
			default:
				return fmt.Errorf("unsupported contract type %q", typ)
			}
		}
	}
	return nil
}

func NewContractValidator(rules []Rule, contract *Contract) (*Validator, error) {
	v, err := NewValidator(rules)
	if err != nil {
		return nil, err
	}
	if contract != nil {
		if err = contract.Validate(); err != nil {
			return nil, err
		}
		v.contract = contract
	}
	return v, nil
}

func (v *Validator) checkContract(row any) {
	if v.contract == nil {
		return
	}
	issue := func(field, expected, actual string) {
		v.Report.Errors++
		if len(v.Report.Issues) < 200 {
			v.Report.Issues = append(v.Report.Issues, Issue{Row: v.Report.Records, Field: field, Check: "schema", Expected: expected, Actual: actual, Suggestion: "Review the incoming schema or update the contract."})
		} else {
			v.Report.IssuesTruncated = true
		}
	}
	obj, ok := row.(map[string]any)
	if !ok {
		issue("", "object record", Type(row))
		return
	}
	allowed := map[string]bool{}
	for _, col := range v.contract.Columns {
		allowed[col.Name] = true
		value, exists := obj[col.Name]
		if !exists {
			if col.Required {
				issue(col.Name, "column present", "missing")
			}
			continue
		}
		if value == nil && col.Nullable {
			continue
		}
		actual := Type(value)
		match := false
		for _, typ := range col.Types {
			if actual == typ {
				match = true
			}
			if typ == "integer" {
				n, ok := Number(value)
				if ok && n.IsInt() {
					match = true
				}
			}
		}
		if !match {
			issue(col.Name, strings.Join(col.Types, " or "), actual)
		}
	}
	if !v.contract.AllowExtra {
		keys := []string{}
		for key := range obj {
			if !allowed[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			issue(key, "declared column", "unexpected column")
		}
	}
}
