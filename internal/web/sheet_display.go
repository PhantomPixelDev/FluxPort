package web

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
)

type SheetCell struct{ Kind, Text, Raw string }
type SheetColumn struct {
	Name, Type, SortURL, Direction string
	Rank                           int
	// One menu per column (followups 5): explicit sort direction and a
	// filter-this-column query, both built from the same state as SortURL.
	SortAscURL, SortDescURL, FilterURL string
}

type SheetPage struct {
	Number       int
	URL          string
	Current, Gap bool
}

// Keep navigation bounded even for datasets with millions of pages.
func sheetPages(current, total int, id string, q collections.Query) []SheetPage {
	if total < 1 {
		total = 1
	}
	current = max(1, min(current, total))
	q.Page = 1
	b, _ := json.Marshal(q)
	base := "/collections/" + id + "?query=" + url.QueryEscape(string(b)) + "&page="
	pages := []SheetPage{}
	previous := 0
	for n := 1; n <= total; {
		if previous > 0 && n > previous+1 {
			pages = append(pages, SheetPage{Gap: true})
		}
		pages = append(pages, SheetPage{Number: n, URL: base + strconv.Itoa(n), Current: n == current})
		previous = n
		switch {
		case n == total:
			return pages
		case total <= 7:
			n++
		case n < current-2:
			n = max(2, current-2)
		case n >= current+2:
			n = total
		default:
			n++
		}
	}
	return pages
}

func sheetCell(value any, exists bool) SheetCell {
	if !exists {
		return SheetCell{Kind: "missing", Text: "Missing"}
	}
	if value == nil {
		return SheetCell{Kind: "null", Text: "null", Raw: "null"}
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return SheetCell{Kind: "empty", Text: "Empty text"}
		}
		return SheetCell{Kind: "text", Text: shortCell(text), Raw: text}
	}
	b, _ := conversion.MarshalJSON(value)
	raw := string(b)
	switch value.(type) {
	case json.Number, float64, float32, int, int64, int32, uint, uint64:
		return SheetCell{Kind: "number", Text: raw, Raw: raw}
	case bool:
		return SheetCell{Kind: "boolean", Text: raw, Raw: raw}
	default:
		return SheetCell{Kind: "json", Text: "JSON", Raw: raw}
	}
}
func shortCell(text string) string {
	if utf8.RuneCountInString(text) <= 256 {
		return text
	}
	return string([]rune(text)[:256]) + "…"
}
func sheetType(types string) string {
	labels := map[string]bool{}
	for _, kind := range strings.Split(types, ",") {
		switch strings.TrimSpace(kind) {
		case "string":
			labels["Text"] = true
		case "number":
			labels["Number"] = true
		case "boolean":
			labels["Boolean"] = true
		case "object", "array":
			labels["JSON"] = true
		}
	}
	if len(labels) > 1 {
		return "Mixed"
	}
	for label := range labels {
		return label
	}
	return "JSON"
}
func sheetHeaders(fields []collections.Field) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, preferred := range []string{"id", "name", "email", "sku", "status", "event_type", "timestamp"} {
		for _, field := range fields {
			if field.Name == preferred {
				names = append(names, field.Name)
				seen[field.Name] = true
			}
		}
	}
	for _, field := range fields {
		if !seen[field.Name] && len(names) < 48 {
			names = append(names, field.Name)
			seen[field.Name] = true
		}
	}
	return names
}
func literalColumnPath(name string) string {
	return "/" + strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}
func sheetColumn(name, types, id string, q collections.Query) SheetColumn {
	column := SheetColumn{Name: name, Type: sheetType(types)}
	path := literalColumnPath(name)
	next := q
	next.Sort = append([]collections.Sort{}, q.Sort...)
	next.Page = 1
	found := -1
	for i, sort := range q.Sort {
		if sort.Field == path || (sort.Field == name && !strings.ContainsAny(name, "./~")) {
			found = i
			column.Rank = i + 1
			column.Direction = "ascending"
			if sort.Desc {
				column.Direction = "descending"
			}
			break
		}
	}
	if found < 0 {
		if len(next.Sort) >= 8 {
			return column
		}
		next.Sort = append(next.Sort, collections.Sort{Field: path})
	} else if !next.Sort[found].Desc {
		next.Sort[found].Desc = true
	} else {
		next.Sort = append(next.Sort[:found], next.Sort[found+1:]...)
	}
	b, _ := json.Marshal(next)
	column.SortURL = "/collections/" + id + "?query=" + url.QueryEscape(string(b))
	// Explicit directions for the per-column menu: this column becomes the only
	// sort, so the direction the user picks is the direction they get.
	asc := q
	asc.Sort = []collections.Sort{{Field: path}}
	desc := asc
	desc.Sort = []collections.Sort{{Field: path, Desc: true}}
	ascJSON, _ := json.Marshal(asc)
	descJSON, _ := json.Marshal(desc)
	column.SortAscURL = "/collections/" + id + "?query=" + url.QueryEscape(string(ascJSON))
	column.SortDescURL = "/collections/" + id + "?query=" + url.QueryEscape(string(descJSON))
	filtered := q
	filtered.Page = 1
	filtered.Filters = []collections.Filter{{Field: path, Op: "eq"}}
	filteredJSON, _ := json.Marshal(filtered)
	column.FilterURL = "/collections/" + id + "?query=" + url.QueryEscape(string(filteredJSON))
	return column
}
