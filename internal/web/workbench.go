package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

type SheetRow struct {
	ID      string
	Ordinal int64
	Cells   []string
	JSON    string
	Kinds   []string
	Display []SheetCell
}
type SheetFilter struct{ Field, Op, Value, Label string }
type Workbench struct {
	Query                              collections.Query
	QueryJSON, Generation, ColumnsJSON string
	Filters                            []SheetFilter
	Views                              []collections.View
	Rows                               []SheetRow
	Columns                            []SheetColumn
	Pages                              []SheetPage
	Scalar                             bool
}

func decodeQuery(text string) (collections.Query, error) {
	q := collections.Query{Limit: 50}
	if text == "" {
		return q, nil
	}
	if len(text) > 16384 {
		return q, fmt.Errorf("query exceeds 16 KiB")
	}
	if _, err := conversion.Decode([]byte(text), "json"); err != nil {
		return q, err
	}
	d := json.NewDecoder(bytes.NewBufferString(text))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(&q); err != nil {
		return q, err
	}
	return q, collections.ValidateQuery(q)
}
func (s *Server) workbench(c fiber.Ctx) error {
	return s.detail(c)
}
func (s *Server) fillWorkbench(c fiber.Ctx, p *Page, col collections.Collection) error {
	var err error
	w := &Workbench{ColumnsJSON: "null"}
	p.Workbench = w
	w.Views, err = s.Service.Views(col.ID)
	if err != nil {
		return err
	}
	query := c.Query("query")
	if v, ok := c.Locals("workbench_query").(string); ok {
		query = v
	}
	w.Query, err = decodeQuery(query)
	if err != nil {
		return fiber.NewError(400, err.Error())
	}
	if query == "" {
		w.Query.Limit = 20
		w.Query.Search = c.Query("q")
		if n, e := strconv.Atoi(c.Query("limit")); e == nil && (n == 20 || n == 50 || n == 100) {
			w.Query.Limit = n
		}
	}
	if c.Request().URI().QueryArgs().Has("q") {
		w.Query.Search = c.Query("q")
	}
	if n, e := strconv.Atoi(c.Query("limit")); e == nil && (n == 20 || n == 50 || n == 100) {
		w.Query.Limit = n
	}
	if id := c.Query("view"); id != "" {
		found := false
		for _, view := range w.Views {
			if view.ID == id {
				w.Query = view.Query
				columns, _ := json.Marshal(view.Columns)
				w.ColumnsJSON = string(columns)
				found = true
				break
			}
		}
		if !found {
			return fiber.NewError(404, "Saved view not found.")
		}
	}
	if n, e := strconv.Atoi(c.Query("page")); e == nil && n > 0 {
		w.Query.Page = n
	}
	if message, ok := c.Locals("workbench_message").(string); ok {
		p.Message = message
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	result, err := s.Service.Query(ctx, col, w.Query)
	if err != nil {
		p.Message = err.Error()
		p.Pages = 1
	} else {
		w.Generation = result.Generation
		w.Query.Page = result.Page
		w.Query.Limit = result.Limit
		p.Total = int(result.Total)
		p.Page = result.Page
		p.Limit = result.Limit
		p.Pages = max(1, (p.Total+p.Limit-1)/p.Limit)
		if p.Page > 1 {
			p.Prev = p.Page - 1
		}
		if p.Page < p.Pages {
			p.Next = p.Page + 1
		}
		p.Fields, _ = s.Service.Fields(col)
		values := make([]any, len(result.Records))
		for i, row := range result.Records {
			values[i] = row.Value
		}
		p.Headers = sheetHeaders(p.Fields)
		w.Scalar = len(p.Headers) == 0
		if len(p.Headers) == 0 && len(values) > 0 {
			p.Headers = []string{"Value"}
		}
		types := map[string]string{}
		for _, field := range p.Fields {
			types[field.Name] = field.Types
		}
		for _, name := range p.Headers {
			column := sheetColumn(name, types[name], col.ID, w.Query)
			if w.Scalar {
				column.SortURL = ""
			}
			w.Columns = append(w.Columns, column)
		}
		for _, row := range result.Records {
			b, _ := conversion.Encode(row.Value, "json")
			cells := []string{}
			display := []SheetCell{}
			kinds := []string{}
			for _, header := range p.Headers {
				v, exists := row.Value, true
				if object, ok := row.Value.(map[string]any); ok {
					v, exists = object[header]
				}
				kind := "json"
				if _, ok := v.(string); ok {
					kind = "string"
				}
				kinds = append(kinds, kind)
				cell := sheetCell(v, exists)
				display = append(display, cell)
				cells = append(cells, cell.Raw)
			}
			w.Rows = append(w.Rows, SheetRow{ID: row.ID, Ordinal: row.Ordinal, Cells: cells, JSON: string(b), Kinds: kinds, Display: display})
		}
	}
	qbytes, _ := json.Marshal(w.Query)
	p.Collection, err = s.Service.Get(col.ID)
	if err != nil {
		return err
	}
	w.QueryJSON = string(qbytes)
	w.Pages = sheetPages(p.Page, p.Pages, col.ID, w.Query)
	for _, filter := range w.Query.Filters {
		value, _ := json.Marshal(filter.Value)
		label := map[string]string{"eq": "equals", "neq": "does not equal", "contains": "contains", "starts": "starts with", "gt": "greater than", "gte": "at least", "lt": "less than", "lte": "at most", "null": "missing / null", "notnull": "has a value"}[filter.Op]
		w.Filters = append(w.Filters, SheetFilter{Field: filter.Field, Op: filter.Op, Value: string(value), Label: label})
	}
	return nil
}
func (s *Server) workbenchStatus(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": col.Status, "generation": col.Generation, "error": col.Error})
}
func (s *Server) workbenchFailure(c fiber.Ctx, err error) error {
	status := 422
	if errors.Is(err, collections.ErrConflict) {
		status = 409
	}
	if errors.Is(err, collections.ErrPending) {
		status = 503
		c.Set("Retry-After", "2")
	}
	c.Status(status)
	c.Locals("workbench_message", err.Error())
	c.Locals("workbench_query", c.FormValue("query"))
	return s.workbench(c)
}
func (s *Server) editRecords(c fiber.Ctx) error {
	changes := []collections.Mutation{}
	method := c.FormValue("method")
	if method != "PATCH" && method != "PUT" && method != "DELETE" && method != "FIELD" {
		return s.workbenchFailure(c, fmt.Errorf("invalid edit action"))
	}
	var value any
	if method != "DELETE" {
		var err error
		value, err = conversion.Decode([]byte(c.FormValue("value")), "json")
		if err != nil {
			return s.workbenchFailure(c, err)
		}
	}
	ids := c.Request().PostArgs().PeekMulti("record_id")
	for _, id := range ids {
		changes = append(changes, collections.Mutation{ID: string(id), Method: method, Value: value})
	}
	if _, err := s.Service.BulkMutate(c.Params("id"), c.FormValue("generation"), changes); err != nil {
		return s.workbenchFailure(c, err)
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id") + "/workbench")
}
func (s *Server) saveWorkbenchView(c fiber.Ctx) error {
	q, err := decodeQuery(c.FormValue("query"))
	if err != nil {
		return s.workbenchFailure(c, err)
	}
	columns := []string{}
	for _, v := range c.Request().PostArgs().PeekMulti("columns") {
		columns = append(columns, string(v))
	}
	v, err := s.Service.SaveView(collections.View{CollectionID: c.Params("id"), Name: c.FormValue("name"), Query: q, Columns: columns})
	if err != nil {
		return s.workbenchFailure(c, err)
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id") + "/workbench?view=" + v.ID)
}
func (s *Server) deleteWorkbenchView(c fiber.Ctx) error {
	if err := s.Service.DeleteView(c.Params("id"), c.Params("view")); err != nil {
		return err
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id") + "/workbench")
}
func (s *Server) exportWorkbench(c fiber.Ctx) error {
	q, err := decodeQuery(c.FormValue("query"))
	if err != nil {
		return s.workbenchFailure(c, err)
	}
	job, err := s.Service.ExportQueryJob(c.Params("id"), c.FormValue("generation"), c.FormValue("format"), q)
	if err != nil {
		return s.workbenchFailure(c, err)
	}
	if c.FormValue("download_view") == "compact" {
		return s.compactDownload(c, job)
	}
	return c.Redirect().Status(303).To("/converter?collection=" + c.Params("id") + "#activity")
}
