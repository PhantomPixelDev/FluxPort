package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"fluxport/internal/conversion"
)

type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}
type Sort struct {
	Field string `json:"field"`
	Desc  bool   `json:"desc,omitempty"`
}
type Query struct {
	Search  string   `json:"search,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
	Sort    []Sort   `json:"sort,omitempty"`
	Page    int      `json:"page,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}
type Record struct {
	ID      string `json:"id"`
	Ordinal int64  `json:"ordinal"`
	Value   any    `json:"value"`
}
type QueryResult struct {
	Records    []Record `json:"records"`
	Total      int64    `json:"total"`
	Generation string   `json:"generation"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
}

// Field paths are data, never SQL fragments. Dots address nested object keys.
func jsonPath(field string) (string, error) {
	if field == "" || len(field) > 256 || strings.ContainsAny(field, "\x00\r\n") {
		return "", fmt.Errorf("invalid field path")
	}
	parts, err := fieldParts(field)
	if err != nil {
		return "", err
	}
	path := "$"
	for _, part := range parts {
		if (part == "" && !strings.HasPrefix(field, "/")) || strings.ContainsAny(part, "\"\\") {
			return "", fmt.Errorf("invalid field path %q", field)
		}
		path += ".\"" + part + "\""
	}
	return path, nil
}
func fieldParts(field string) ([]string, error) {
	if strings.HasPrefix(field, "/") {
		parts := strings.Split(field[1:], "/")
		for i, part := range parts {
			for j := 0; j < len(part); j++ {
				if part[j] == '~' {
					if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
						return nil, fmt.Errorf("invalid JSON pointer")
					}
					j++
				}
			}
			parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		}
		return parts, nil
	}
	return strings.Split(field, "."), nil
}
func querySQL(q Query) (where, order string, args, sortArgs []any, err error) {
	if q.Page < 0 || q.Page > 1000000000 || q.Limit < 0 || q.Limit > 100 {
		err = fmt.Errorf("page must be between 1 and 1000000000 and limit between 1 and 100")
		return
	}
	if len(q.Search) > 256 || len(q.Filters) > 20 || len(q.Sort) > 8 {
		err = fmt.Errorf("query exceeds limits: 256 search characters, 20 filters, 8 sorts")
		return
	}
	clauses := []string{}
	if q.Search != "" {
		clauses = append(clauses, `instr(lower(payload),lower(?))>0`)
		args = append(args, q.Search)
	}
	for _, f := range q.Filters {
		var path string
		path, err = jsonPath(f.Field)
		if err != nil {
			return
		}
		expr := `json_extract(payload,?)`
		args = append(args, path)
		var value any = f.Value
		if text, ok := value.(string); ok && len(text) > 4096 {
			err = fmt.Errorf("filter values are limited to 4096 bytes")
			return
		}
		if n, ok := value.(json.Number); ok {
			if i, e := n.Int64(); e == nil {
				value = i
			} else if !strings.ContainsAny(n.String(), ".eE") {
				err = fmt.Errorf("numeric filters support signed 64-bit integers; use text values for larger identifiers")
				return
			} else if d, e := n.Float64(); e == nil {
				value = d
			} else {
				err = fmt.Errorf("invalid number")
				return
			}
		}
		switch value.(type) {
		case nil, string, bool, int, int64, float64:
		default:
			err = fmt.Errorf("filter values must be scalars")
			return
		}
		switch f.Op {
		case "eq", "neq", "gt", "gte", "lt", "lte":
			operator := map[string]string{"eq": " IS ", "neq": " IS ", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[f.Op]
			clause := expr + operator + "?"
			args = append(args, value)
			if value != nil {
				typeCheck := `COALESCE(json_type(payload,?),'missing')`
				args = append(args, path)
				switch typed := value.(type) {
				case string:
					typeCheck += `='text'`
				case bool:
					if typed {
						typeCheck += `='true'`
					} else {
						typeCheck += `='false'`
					}
				default:
					typeCheck += ` IN ('integer','real')`
				}
				clause = "(" + clause + " AND " + typeCheck + ")"
			}
			if f.Op == "neq" {
				clause = "NOT (" + clause + ")"
			}
			clauses = append(clauses, clause)
		case "contains":
			clauses = append(clauses, `instr(lower(CAST(`+expr+` AS TEXT)),lower(?))>0`)
			args = append(args, fmt.Sprint(value))
		case "starts":
			clauses = append(clauses, `substr(lower(CAST(`+expr+` AS TEXT)),1,length(?))=lower(?)`)
			args = append(args, fmt.Sprint(value), fmt.Sprint(value))
		case "null":
			clauses = append(clauses, expr+` IS NULL`)
		case "notnull":
			clauses = append(clauses, expr+` IS NOT NULL`)
		default:
			err = fmt.Errorf("unsupported filter operator %q", f.Op)
			return
		}
	}
	where = "1"
	if len(clauses) > 0 {
		where = strings.Join(clauses, " AND ")
	}
	orders := []string{}
	for _, sort := range q.Sort {
		var path string
		path, err = jsonPath(sort.Field)
		if err != nil {
			return
		}
		direction := " ASC"
		if sort.Desc {
			direction = " DESC"
		}
		orders = append(orders, `json_extract(payload,?)`+direction)
		sortArgs = append(sortArgs, path)
	}
	orders = append(orders, "ordinal ASC")
	order = strings.Join(orders, ",")
	return
}
func ValidateQuery(q Query) error { _, _, _, _, err := querySQL(q); return err }

func (s *Service) Query(ctx context.Context, c Collection, q Query) (QueryResult, error) {
	out := QueryResult{Records: []Record{}}
	_, _, _, _, err := querySQL(q)
	if err != nil {
		return out, err
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 || q.Limit > 100 {
		q.Limit = 50
	}
	if q.Page > 1000000000 {
		return out, fmt.Errorf("page exceeds limit")
	}
	ref, err := s.sourceReference(c, "")
	if err != nil {
		return out, err
	}
	defer ref.release()
	return s.queryReference(ctx, ref, q)
}

func (s *Service) queryReference(ctx context.Context, ref indexReference, q Query) (QueryResult, error) {
	out := QueryResult{Records: []Record{}}
	where, order, args, sortArgs, err := querySQL(q)
	if err != nil {
		return out, err
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 50
	}
	db, err := openReference(ref)
	if err != nil {
		return out, err
	}
	defer db.Close()
	out.Generation = ref.generation
	out.Page = q.Page
	out.Limit = q.Limit
	out.Total = ref.rows
	if where != "1" {
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE `+where, args...).Scan(&out.Total); err != nil {
			return out, err
		}
	}
	all := append(append([]any{}, args...), sortArgs...)
	all = append(all, q.Limit, int64(q.Page-1)*int64(q.Limit))
	statement := `SELECT record_id,ordinal,payload FROM records WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	if where == "1" && len(q.Sort) == 0 {
		// Published indexes have contiguous ordinals: seek ordinary later pages.
		statement = `SELECT record_id,ordinal,payload FROM records WHERE ordinal>=? ORDER BY ordinal LIMIT ?`
		all = []any{int64(q.Page-1) * int64(q.Limit), q.Limit}
	}
	rows, err := db.QueryContext(ctx, statement, all...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row Record
		var payload string
		if err = rows.Scan(&row.ID, &row.Ordinal, &payload); err != nil {
			return out, err
		}
		row.Value, err = conversion.Decode([]byte(payload), "json")
		if err != nil {
			return out, err
		}
		out.Records = append(out.Records, row)
	}
	return out, rows.Err()
}

// EachQuery streams the full filtered result from a single published generation.
func (s *Service) EachQuery(ctx context.Context, c Collection, q Query, generation string, emit func(any) error) error {
	_, _, _, _, err := querySQL(q)
	if err != nil {
		return err
	}
	ref, err := s.sourceReference(c, generation)
	if err != nil {
		return err
	}
	defer ref.release()
	return s.eachReference(ctx, ref, q, emit)
}

func (s *Service) eachReference(ctx context.Context, ref indexReference, q Query, emit func(any) error) error {
	where, order, args, sortArgs, err := querySQL(NormalizeInputQuery(q))
	if err != nil {
		return err
	}
	db, err := openReference(ref)
	if err != nil {
		return err
	}
	defer db.Close()
	all := append(append([]any{}, args...), sortArgs...)
	rows, err := db.QueryContext(ctx, `SELECT payload FROM records WHERE `+where+` ORDER BY `+order, all...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return err
		}
		var b string
		if err = rows.Scan(&b); err != nil {
			return err
		}
		v, e := conversion.Decode([]byte(b), "json")
		if e != nil {
			return e
		}
		if err = emit(v); err != nil {
			return err
		}
	}
	return rows.Err()
}
