package collections

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"fluxport/internal/conversion"
	"fluxport/internal/quality"
	"fluxport/internal/transformations"
)

type Aggregate struct {
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
	As    string `json:"as"`
}
type PipelineJoin struct {
	Collection string `json:"collection"`
	Left       string `json:"left"`
	Right      string `json:"right"`
	As         string `json:"as"`
	Kind       string `json:"kind,omitempty"`
}
type PipelineStep struct {
	Transform  *transformations.Operation `json:"transform,omitempty"`
	Limit      int                        `json:"limit,omitempty"`
	Append     string                     `json:"append,omitempty"`
	Op         string                     `json:"op"`
	Filters    []Filter                   `json:"filters,omitempty"`
	Rules      transformations.Rules      `json:"rules,omitempty"`
	Set        map[string]any             `json:"set,omitempty"`
	Copy       map[string]string          `json:"copy,omitempty"`
	Sort       []Sort                     `json:"sort,omitempty"`
	Keys       []string                   `json:"keys,omitempty"`
	Aggregates []Aggregate                `json:"aggregates,omitempty"`
	Join       *PipelineJoin              `json:"join,omitempty"`
}
type Pipeline struct {
	InputQuery *Query         `json:"input_query,omitempty"`
	Revision   int            `json:"revision,omitempty"`
	Validation []quality.Rule `json:"validation,omitempty"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Source     string         `json:"source"`
	Steps      []PipelineStep `json:"steps"`
	Updated    string         `json:"updated,omitempty"`
	Version    int            `json:"version"`
}

func sqlLiteral(v any) (string, error) {
	if v == nil {
		return "NULL", nil
	}
	switch value := v.(type) {
	case string:
		return quoteSQL(value), nil
	case bool:
		if value {
			return "1", nil
		}
		return "0", nil
	case int:
		return strconv.Itoa(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case float64:
		if _, e := json.Marshal(value); e != nil {
			return "", e
		}
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case json.Number:
		if _, e := json.Marshal(value); e != nil {
			return "", e
		}
		return value.String(), nil
	default:
		return "", fmt.Errorf("SQL parameter must be a scalar")
	}
}
func bindPipelineSQL(text string, args []any) (string, error) {
	// Generated statements contain placeholders only in expressions we own.
	parts := strings.Split(text, "?")
	if len(parts) != len(args)+1 {
		return "", fmt.Errorf("pipeline parameter mismatch")
	}
	var out strings.Builder
	out.WriteString(parts[0])
	for i, value := range args {
		literal, err := sqlLiteral(value)
		if err != nil {
			return "", err
		}
		out.WriteString(literal)
		out.WriteString(parts[i+1])
	}
	return out.String(), nil
}
func pipelinePath(field string) (string, error) {
	path, err := jsonPath(field)
	return quoteSQL(path), err
}
func pipelineFilterWhere(filters []Filter, legacy map[string]any) (string, error) {
	if len(filters)+len(legacy) > 20 {
		return "", fmt.Errorf("a filter step supports up to 20 conditions")
	}
	where, _, args, _, err := querySQL(Query{Filters: filters})
	if err != nil {
		return "", err
	}
	where, err = bindPipelineSQL(where, args)
	if err != nil {
		return "", err
	}
	keys := []string{}
	for key := range legacy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		where = "(" + where + ") AND json_type(payload)='object'"
	}
	for _, key := range keys {
		path, e := pipelinePath("/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"))
		if e != nil {
			return "", e
		}
		value, e := conversion.MarshalJSON(legacy[key])
		if e != nil {
			return "", e
		}
		where += ` AND COALESCE(payload -> ` + path + `,'null')=json(` + quoteSQL(string(value)) + `)`
	}
	return where, nil
}
func pipelineRuleExpression(r transformations.Rules) (string, error) {
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if _, err = transformations.Parse(string(encoded)); err != nil {
		return "", err
	}
	conditions := []string{}
	literalList := func(fields []string) string {
		out := []string{}
		for _, field := range fields {
			out = append(out, quoteSQL(field))
		}
		return strings.Join(out, ",")
	}
	if len(r.Select) > 0 {
		conditions = append(conditions, "j.key IN ("+literalList(r.Select)+")")
	}
	if len(r.Drop) > 0 {
		conditions = append(conditions, "j.key NOT IN ("+literalList(r.Drop)+")")
	}
	key := "j.key"
	sources := []string{}
	destinations := map[string]bool{}
	for from := range r.Rename {
		sources = append(sources, from)
	}
	sort.Strings(sources)
	if len(sources) > 0 {
		key = "CASE j.key"
		for _, from := range sources {
			to := r.Rename[from]
			if destinations[to] {
				return "", fmt.Errorf("rename destinations must be unique")
			}
			destinations[to] = true
			key += " WHEN " + quoteSQL(from) + " THEN " + quoteSQL(to)
			eligible := len(r.Select) == 0
			for _, field := range r.Select {
				if field == from {
					eligible = true
				}
			}
			for _, field := range r.Drop {
				if field == from {
					eligible = false
				}
			}
			if _, isSource := r.Rename[to]; !isSource && eligible {
				path, e := pipelinePath("/" + strings.ReplaceAll(strings.ReplaceAll(from, "~", "~0"), "/", "~1"))
				if e != nil {
					return "", e
				}
				conditions = append(conditions, "NOT (j.key="+quoteSQL(to)+" AND json_type(payload,"+path+") IS NOT NULL)")
			}
		}
		key += " ELSE j.key END"
	}
	where := "1"
	if len(conditions) > 0 {
		where = strings.Join(conditions, " AND ")
	}
	// The -> operator extracts JSON text, preserving booleans, nested values and
	// large integer lexemes instead of coercing them into SQLite scalar numbers.
	return `CASE WHEN json_type(payload)='object' THEN (SELECT json_group_object(` + key + `,json(payload -> j.fullkey)) FROM json_each(payload) j WHERE ` + where + `) ELSE payload END`, nil
}

// CompilePipeline produces SELECT-only CTEs over disposable source indexes.
// Global operations use SQLite's disk-backed sorting and grouping.
func CompilePipeline(p Pipeline) (string, []string, error) {
	if p.InputQuery != nil {
		q := NormalizeInputQuery(*p.InputQuery)
		p.InputQuery = nil
		text, ids, err := CompilePipeline(p)
		if err != nil {
			return "", nil, err
		}
		root, args, err := pipelineInputSQL(p.Source, q)
		if err != nil {
			return "", nil, err
		}
		root, err = bindPipelineSQL(root, args)
		return strings.Replace(text, pipelineRootSQL(p.Source), root, 1), ids, err
	}
	if _, err := quality.NewValidator(p.Validation); err != nil {
		return "", nil, err
	}
	if p.Version != 0 && p.Version != 1 {
		return "", nil, fmt.Errorf("unsupported pipeline version")
	}
	if p.Source == "" || len(p.Steps) > 20 {
		return "", nil, fmt.Errorf("source is required; pipelines support at most 20 steps")
	}
	ids := []string{p.Source}
	seen := map[string]bool{p.Source: true}
	ctes := []string{`s0 AS (SELECT _ordinal AS position,_json AS payload FROM ` + quoteIdentifier("c_"+p.Source) + `)`}
	for i, step := range p.Steps {
		previous := fmt.Sprintf("s%d", i)
		body := ""
		var err error
		switch step.Op {
		case "transform":
			if step.Transform == nil {
				return "", nil, fmt.Errorf("step %d requires transform configuration", i+1)
			}
			if e := step.Transform.Validate(); e != nil {
				return "", nil, fmt.Errorf("step %d: %w", i+1, e)
			}
			b, e := json.Marshal(step.Transform)
			if e != nil {
				return "", nil, e
			}
			body = `SELECT position,fluxport_transform(payload,` + quoteSQL(string(b)) + `) AS payload FROM ` + previous
		case "limit":
			if step.Limit < 1 || step.Limit > 1000000000 {
				return "", nil, fmt.Errorf("step %d limit must be 1–1000000000", i+1)
			}
			body = `SELECT position,payload FROM ` + previous + ` ORDER BY position LIMIT ` + strconv.Itoa(step.Limit)
		case "append":
			if step.Append == "" {
				return "", nil, fmt.Errorf("step %d requires an append collection", i+1)
			}
			if !seen[step.Append] {
				ids = append(ids, step.Append)
				seen[step.Append] = true
			}
			if len(ids) > 8 {
				return "", nil, fmt.Errorf("pipeline references more than eight collections")
			}
			body = `SELECT ROW_NUMBER() OVER (ORDER BY source,position) AS position,payload FROM (SELECT 0 AS source,position,payload FROM ` + previous + ` UNION ALL SELECT 1 AS source,_ordinal AS position,_json AS payload FROM ` + quoteIdentifier("c_"+step.Append) + `)`
		case "filter":
			where, e := pipelineFilterWhere(step.Filters, step.Rules.Filter)
			if e != nil {
				err = e
				break
			}
			body = `SELECT position,payload FROM ` + previous + ` WHERE ` + where
		case "select", "drop", "rename":
			expression, e := pipelineRuleExpression(step.Rules)
			err = e
			body = `SELECT position,` + expression + ` AS payload FROM ` + previous
			if err == nil && len(step.Rules.Filter) > 0 {
				where, e := pipelineFilterWhere(nil, step.Rules.Filter)
				err = e
				body += ` WHERE ` + where
			}
		case "map":
			if len(step.Set)+len(step.Copy) > 16 {
				err = fmt.Errorf("map supports up to 16 target fields")
				break
			}
			expression := "payload"
			keys := []string{}
			for key := range step.Set {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				path, e := pipelinePath(key)
				if e != nil {
					err = e
					break
				}
				b, e := conversion.MarshalJSON(step.Set[key])
				if e != nil {
					err = e
					break
				}
				expression = `json_set(` + expression + `,` + path + `,json(` + quoteSQL(string(b)) + `))`
			}
			keys = nil
			for key := range step.Copy {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				target, e := pipelinePath(key)
				if e != nil {
					err = e
					break
				}
				source, e := pipelinePath(step.Copy[key])
				if e != nil {
					err = e
					break
				}
				expression = `CASE WHEN json_type(payload,` + source + `) IS NULL THEN ` + expression + ` ELSE json_set(` + expression + `,` + target + `,json(payload -> ` + source + `)) END`
			}
			body = `SELECT position,` + expression + ` AS payload FROM ` + previous
		case "sort":
			_, order, _, args, e := querySQL(Query{Sort: step.Sort})
			if e != nil {
				err = e
				break
			}
			order = strings.ReplaceAll(order, "ordinal ASC", "position ASC")
			order, err = bindPipelineSQL(order, args)
			body = `SELECT ROW_NUMBER() OVER (ORDER BY ` + order + `) AS position,payload FROM ` + previous
		case "deduplicate":
			group := []string{}
			for _, field := range step.Keys {
				path, e := pipelinePath(field)
				if e != nil {
					err = e
					break
				}
				group = append(group, `payload -> `+path)
			}
			if len(group) == 0 {
				group = append(group, "payload")
			}
			body = `SELECT MIN(position) AS position,payload FROM ` + previous + ` GROUP BY ` + strings.Join(group, ",")
		case "group", "aggregate":
			if len(step.Keys) > 16 || len(step.Aggregates) > 16 || len(step.Aggregates) == 0 {
				err = fmt.Errorf("grouping requires 1 to 16 aggregates and up to 16 keys")
				break
			}
			group := []string{}
			pairs := []string{}
			names := map[string]bool{}
			for _, field := range step.Keys {
				path, e := pipelinePath(field)
				if e != nil {
					err = e
					break
				}
				group = append(group, `payload -> `+path)
				pairs = append(pairs, quoteSQL(field), `json(payload -> `+path+`)`)
				names[field] = true
			}
			for _, aggregate := range step.Aggregates {
				if aggregate.As == "" || names[aggregate.As] {
					err = fmt.Errorf("aggregate aliases must be non-empty and unique")
					break
				}
				names[aggregate.As] = true
				operation := strings.ToUpper(aggregate.Op)
				expression := ""
				if operation == "COUNT" {
					expression = "COUNT(*)"
				} else {
					if operation != "SUM" && operation != "AVG" && operation != "MIN" && operation != "MAX" {
						err = fmt.Errorf("unsupported aggregate %q", aggregate.Op)
						break
					}
					path, e := pipelinePath(aggregate.Field)
					if e != nil {
						err = e
						break
					}
					expression = operation + `(CASE WHEN json_type(payload,` + path + `) IN ('integer','real') THEN json_extract(payload,` + path + `) END)`
				}
				pairs = append(pairs, quoteSQL(aggregate.As), expression)
			}
			body = `SELECT COALESCE(MIN(position),0) AS position,json_object(` + strings.Join(pairs, ",") + `) AS payload FROM ` + previous
			if len(group) > 0 {
				body += ` GROUP BY ` + strings.Join(group, ",")
			}
		case "join":
			j := step.Join
			if j == nil || j.Collection == "" || j.As == "" {
				err = fmt.Errorf("join requires a collection, fields and target field")
				break
			}
			kind := "INNER"
			if j.Kind == "left" {
				kind = "LEFT"
			} else if j.Kind != "" && j.Kind != "inner" {
				err = fmt.Errorf("join kind must be inner or left")
				break
			}
			left, e := pipelinePath(j.Left)
			if e != nil {
				err = e
				break
			}
			right, e := pipelinePath(j.Right)
			if e != nil {
				err = e
				break
			}
			target, e := pipelinePath(j.As)
			if e != nil {
				err = e
				break
			}
			if !seen[j.Collection] {
				ids = append(ids, j.Collection)
				seen[j.Collection] = true
			}
			if len(ids) > 8 {
				err = fmt.Errorf("pipeline references more than eight collections")
				break
			}
			body = `SELECT ROW_NUMBER() OVER (ORDER BY a.position,b._ordinal) AS position,json_set(a.payload,` + target + `,json(b._json)) AS payload FROM ` + previous + ` a ` + kind + ` JOIN ` + quoteIdentifier("c_"+j.Collection) + ` b ON (a.payload -> ` + left + `)=(b._json -> ` + right + `)`
		default:
			err = fmt.Errorf("unsupported operation %q", step.Op)
		}
		if err != nil {
			return "", nil, fmt.Errorf("step %d (%s): %w", i+1, step.Op, err)
		}
		ctes = append(ctes, fmt.Sprintf("s%d AS (%s)", i+1, body))
	}
	text := "WITH " + strings.Join(ctes, ",") + fmt.Sprintf(" SELECT payload FROM s%d ORDER BY position", len(p.Steps))
	if err := ValidateSQL(text); err != nil {
		return "", nil, fmt.Errorf("pipeline exceeds SQL safety limits: %w", err)
	}
	return text, ids, nil
}
