package collections

import "strings"

// NormalizeInputQuery captures the complete ordered view, never its page.
func NormalizeInputQuery(q Query) Query {
	q.Page, q.Limit = 0, 0
	return q
}

func pipelineRootSQL(id string) string {
	return `s0 AS (SELECT _ordinal AS position,_json AS payload FROM ` + quoteIdentifier("c_"+id) + `)`
}

func pipelineInputSQL(id string, q Query) (string, []any, error) {
	where, order, args, sortArgs, err := querySQL(NormalizeInputQuery(q))
	if err != nil {
		return "", nil, err
	}
	// Window positions capture the user's sort before subsequent cleanup steps.
	root := `s0 AS (SELECT row_number() OVER (ORDER BY ` + order + `) AS position,payload FROM (SELECT _ordinal AS ordinal,_json AS payload FROM ` + quoteIdentifier("c_"+id) + `) WHERE ` + where + `)`
	return root, append(append([]any{}, sortArgs...), args...), nil
}

// Internal execution binds input values; public compilation retains its existing API.
func compileInputPipeline(p Pipeline) (string, []string, []any, error) {
	input := p.InputQuery
	p.InputQuery = nil
	text, ids, err := CompilePipeline(p)
	if err != nil || input == nil {
		return text, ids, nil, err
	}
	root, args, err := pipelineInputSQL(p.Source, *input)
	return strings.Replace(text, pipelineRootSQL(p.Source), root, 1), ids, args, err
}
