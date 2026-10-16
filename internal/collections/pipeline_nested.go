package collections

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"

	"fluxport/internal/conversion"
	"modernc.org/sqlite"
)

type NestedStep struct {
	Field   string   `json:"field,omitempty"`
	As      string   `json:"as,omitempty"`
	Value   string   `json:"value,omitempty"`
	ValueAs string   `json:"value_as,omitempty"`
	Columns []string `json:"columns,omitempty"`
}

func init() {
	if err := sqlite.RegisterDeterministicScalarFunction("fluxport_expand", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		payload, ok := args[0].(string)
		if !ok || len(payload) > 1<<20 {
			return nil, fmt.Errorf("invalid expansion record")
		}
		config, ok := args[1].(string)
		if !ok || len(config) > 8192 {
			return nil, fmt.Errorf("invalid expansion configuration")
		}
		row, err := conversion.Decode([]byte(payload), "json")
		if err != nil {
			return nil, err
		}
		var step PipelineStep
		if err = json.Unmarshal([]byte(config), &step); err != nil {
			return nil, err
		}
		rows, err := expandRecord(row, step)
		if err != nil {
			return nil, err
		}
		b, err := conversion.MarshalJSON(rows)
		if len(b) > 1<<20 {
			return nil, fmt.Errorf("expanded record exceeds 1 MiB; narrow the array or columns")
		}
		return string(b), err
	}); err != nil {
		panic(err)
	}
	if err := sqlite.RegisterDeterministicScalarFunction("fluxport_pivot_error", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		return nil, fmt.Errorf("pivot has duplicate cells, missing names, non-text names or undeclared columns; aggregate or filter first")
	}); err != nil {
		panic(err)
	}
}
func expandRecord(row any, step PipelineStep) ([]any, error) {
	obj, ok := row.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expansion requires object records")
	}
	n := step.Nested
	if n == nil {
		return nil, fmt.Errorf("nested configuration is required")
	}
	copyRow := func() map[string]any {
		out := map[string]any{}
		for k, v := range obj {
			out[k] = v
		}
		return out
	}
	rows := []any{}
	switch step.Op {
	case "explode":
		v, exists := obj[n.Field]
		if !exists || v == nil {
			return rows, nil
		}
		values, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("column %q must be an array", n.Field)
		}
		if len(values) > 10000 {
			return nil, fmt.Errorf("one array may expand into at most 10000 rows")
		}
		as := n.As
		if as == "" {
			as = n.Field
		}
		if as != n.Field {
			if _, exists := obj[as]; exists {
				return nil, fmt.Errorf("expansion target %q already exists", as)
			}
		}
		for _, value := range values {
			out := copyRow()
			out[as] = value
			rows = append(rows, out)
		}
	case "unpivot":
		name, value := n.As, n.ValueAs
		if name == "" {
			name = "column"
		}
		if value == "" {
			value = "value"
		}
		if name == value {
			return nil, fmt.Errorf("unpivot target columns must differ")
		}
		selected := map[string]bool{}
		for _, k := range n.Columns {
			selected[k] = true
		}
		for _, target := range []string{name, value} {
			if _, exists := obj[target]; exists && !selected[target] {
				return nil, fmt.Errorf("unpivot target %q already exists", target)
			}
		}
		for _, key := range n.Columns {
			v, exists := obj[key]
			if !exists {
				continue
			}
			out := copyRow()
			for k := range selected {
				delete(out, k)
			}
			out[name], out[value] = key, v
			rows = append(rows, out)
		}
	default:
		return nil, fmt.Errorf("unsupported expansion")
	}
	return rows, nil
}
func nestedPipelineSQL(previous string, step PipelineStep) (string, error) {
	n := step.Nested
	if n == nil {
		return "", fmt.Errorf("nested step configuration is required")
	}
	for _, name := range append(append([]string{n.Field, n.As, n.Value, n.ValueAs}, n.Columns...), step.Keys...) {
		if len(name) > 256 {
			return "", fmt.Errorf("nested column name exceeds 256 bytes")
		}
	}
	if len(n.Columns) > 128 || len(step.Keys) > 16 {
		return "", fmt.Errorf("nested operations support 128 columns and 16 grouping keys")
	}
	if step.Op == "explode" && n.Field == "" {
		return "", fmt.Errorf("choose an array column")
	}
	if step.Op != "explode" && len(n.Columns) == 0 {
		return "", fmt.Errorf("choose output columns explicitly")
	}
	seen := map[string]bool{}
	for _, name := range n.Columns {
		if name == "" || seen[name] {
			return "", fmt.Errorf("nested columns must be non-empty and unique")
		}
		seen[name] = true
	}
	if step.Op == "explode" || step.Op == "unpivot" {
		b, err := json.Marshal(step)
		if err != nil {
			return "", err
		}
		return `SELECT row_number() OVER(ORDER BY p.position,CAST(j.key AS INTEGER)) AS position,j.value AS payload FROM ` + previous + ` p,json_each(fluxport_expand(p.payload,` + quoteSQL(string(b)) + `)) j`, nil
	}
	if step.Op != "pivot" || n.Field == "" || n.Value == "" {
		return "", fmt.Errorf("pivot needs name and value columns")
	}
	literalPath := func(name string) (string, error) {
		return pipelinePath("/" + strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1"))
	}
	namePath, err := literalPath(n.Field)
	if err != nil {
		return "", err
	}
	valuePath, err := literalPath(n.Value)
	if err != nil {
		return "", err
	}
	groups := []string{}
	pairs := []string{}
	keyNames := map[string]bool{}
	for _, key := range step.Keys {
		if key == "" || keyNames[key] || seen[key] {
			return "", fmt.Errorf("pivot grouping and output names must be distinct")
		}
		keyNames[key] = true
		path, e := literalPath(key)
		if e != nil {
			return "", e
		}
		groups = append(groups, "COALESCE(payload -> "+path+",'missing')")
		pairs = append(pairs, quoteSQL(key), "json(payload -> "+path+")")
	}
	allowed := []string{}
	for _, name := range n.Columns {
		allowed = append(allowed, quoteSQL(name))
		pairs = append(pairs, quoteSQL(name), "json(MAX(CASE WHEN json_extract(payload,"+namePath+")="+quoteSQL(name)+" THEN COALESCE(payload -> "+valuePath+",'null') END))")
	}
	valid := `COUNT(*)=COUNT(DISTINCT payload -> ` + namePath + `) AND SUM(CASE WHEN json_type(payload,` + namePath + `)='text' AND json_extract(payload,` + namePath + `) IN (` + strings.Join(allowed, ",") + `) THEN 1 ELSE 0 END)=COUNT(*)`
	body := `SELECT MIN(position) AS position,CASE WHEN ` + valid + ` THEN json_object(` + strings.Join(pairs, ",") + `) ELSE fluxport_pivot_error() END AS payload FROM ` + previous
	if len(groups) > 0 {
		body += " GROUP BY " + strings.Join(groups, ",")
	} else {
		body += " HAVING COUNT(*)>0"
	}
	return body, nil
}
