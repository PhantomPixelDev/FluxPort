package web

import (
	"fmt"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

type SQLPage struct {
	Text, Name, Engine   string
	DuckDBTimeoutSeconds int
	Selected             map[string]bool
	Saved                []collections.SavedQuery
	History              []collections.SQLHistory
	Result               *collections.SQLResult
	Published            bool
	DatasetTables        []collections.SQLDataset
}

func (s *Server) sqlPage(c fiber.Ctx) error {
	p := s.base(c, "SQL playground", "sql")
	sq := &SQLPage{Selected: map[string]bool{}, Engine: "auto", DuckDBTimeoutSeconds: int(s.Service.DuckDBTimeout.Seconds())}
	p.SQL = sq
	var err error
	if id := c.Params("id"); id != "" {
		p.Collection, err = s.Service.Get(id)
		if err != nil {
			return err
		}
		p.Active = "collections"
		sq.Selected[id] = true
	}
	p.Collections, _, err = s.Service.List("", 1, 100)
	if err != nil {
		return err
	}
	if p.Collection.ID != "" {
		p.Collections = []collections.Collection{p.Collection}
	}
	sq.Saved, err = s.Service.SavedSQL()
	if err != nil {
		return err
	}
	if p.Collection.ID != "" {
		filtered := sq.Saved[:0]
		for _, query := range sq.Saved {
			if len(query.Collections) == 1 && query.Collections[0] == p.Collection.ID {
				filtered = append(filtered, query)
			}
		}
		sq.Saved = filtered
	}
	sq.History, err = s.Service.SQLHistory()
	if err != nil {
		return err
	}
	if p.Collection.ID != "" {
		sq.History = collectionSQLHistory(sq.History, p.Collection.ID)
	}
	if id := c.Query("saved"); id != "" {
		q, e := s.Service.GetSQL(id)
		if e != nil {
			return fiber.NewError(404, "Saved SQL query not found.")
		}
		if p.Collection.ID != "" && (len(q.Collections) != 1 || q.Collections[0] != p.Collection.ID) {
			return fiber.NewError(404, "Saved query does not belong to this file.")
		}
		sq.Text = q.SQL
		sq.Name = q.Name
		sq.Published = q.Published
		for _, id := range q.Collections {
			sq.Selected[id] = true
		}
	}
	if sq.Text == "" {
		if id := c.Query("collection"); id != "" {
			col, e := s.Service.Get(id)
			if e != nil || col.Status != "ready" {
				return fiber.NewError(404, "Ready collection not found.")
			}
			sq.Selected[id] = true
		}
	}
	if c.Method() == "POST" {
		sq.Text = c.FormValue("sql")
		sq.Name = c.FormValue("name")
		sq.Engine = c.FormValue("engine")
		sq.Published = c.FormValue("published") == "on"
		ids := []string{}
		if p.Collection.ID != "" {
			ids = append(ids, p.Collection.ID)
		}
		for _, v := range c.Request().PostArgs().PeekMulti("collections") {
			id := string(v)
			if p.Collection.ID != "" {
				continue
			}
			ids = append(ids, id)
			sq.Selected[id] = true
		}
		if c.FormValue("action") == "save" {
			_, err = s.Service.SaveSQL(collections.SavedQuery{Name: sq.Name, SQL: sq.Text, Collections: ids, Published: sq.Published})
			if err != nil {
				p.Message = err.Error()
				c.Status(422)
			} else {
				if p.Collection.ID != "" {
					return c.Redirect().Status(303).To("/collections/" + p.Collection.ID + "/sql")
				}
				return c.Redirect().Status(303).To("/sql")
			}
		} else {
			result, e := s.Service.SQLWithEngine(c.Context(), ids, sq.Text, sq.Engine)
			if e == nil {
				sq.Result = &result
			}
			err = e
			if e != nil {
				p.Message = e.Error()
				c.Status(422)
			}
			if e = s.Service.RecordSQL(sq.Text, ids, result.DurationMS, err); e != nil {
				return e
			}
			sq.History, e = s.Service.SQLHistory()
			if e != nil {
				return e
			}
			if p.Collection.ID != "" {
				sq.History = collectionSQLHistory(sq.History, p.Collection.ID)
			}
			if err == nil && c.FormValue("action") == "export" {
				return exportSQL(c, result, c.FormValue("format"))
			}
		}
	}
	for _, col := range p.Collections {
		fields, e := s.Service.Fields(col)
		if e != nil {
			continue
		}
		d := collections.SQLDataset{ID: col.ID, Name: col.Name, Table: "c_" + col.ID, Fields: []string{"_record_id", "_ordinal", "_json"}}
		for _, f := range fields {
			d.Fields = append(d.Fields, f.Name)
		}
		sq.DatasetTables = append(sq.DatasetTables, d)
	}
	if sq.Text == "" && len(sq.DatasetTables) > 0 {
		selected := sq.DatasetTables[0]
		for _, dataset := range sq.DatasetTables {
			if sq.Selected[dataset.ID] {
				selected = dataset
				break
			}
		}
		sq.Text = `SELECT * FROM "` + selected.Table + `" LIMIT 100`
		sq.Selected[selected.ID] = true
	}
	return c.Render("pages/sql", p, "layouts/base")
}
func exportSQL(c fiber.Ctx, result collections.SQLResult, format string) error {
	if result.Truncated {
		return fiber.NewError(422, "Result is truncated. Add a LIMIT or narrow the query before exporting.")
	}
	seen := map[string]bool{}
	for _, name := range result.Columns {
		if seen[name] {
			return fiber.NewError(422, "Use unique aliases for result columns before exporting.")
		}
		seen[name] = true
	}
	values := []any{}
	for _, row := range result.Rows {
		object := map[string]any{}
		for i, value := range row {
			object[result.Columns[i]] = value
		}
		values = append(values, object)
	}
	format = conversion.Format(format)
	if !conversion.Supported(format) {
		return fiber.NewError(422, "Unsupported export format.")
	}
	b, err := conversion.Encode(values, format)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	c.Set("Content-Type", conversion.MIME(format))
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="sql-results.%s"`, format))
	return c.Send(b)
}
func (s *Server) deleteSQL(c fiber.Ctx) error {
	if id := c.FormValue("collection"); id != "" {
		query, err := s.Service.GetSQL(c.Params("query"))
		if err != nil || len(query.Collections) != 1 || query.Collections[0] != id {
			return fiber.NewError(404, "Saved query does not belong to this file.")
		}
		if err := s.Service.DeleteSQL(query.ID); err != nil {
			return err
		}
		return c.Redirect().Status(303).To("/collections/" + id + "/sql")
	}
	if err := s.Service.DeleteSQL(c.Params("query")); err != nil {
		return err
	}
	return c.Redirect().Status(303).To("/sql")
}

func collectionSQLHistory(history []collections.SQLHistory, id string) []collections.SQLHistory {
	filtered := history[:0]
	for _, query := range history {
		if len(query.Collections) == 1 && query.Collections[0] == id {
			filtered = append(filtered, query)
		}
	}
	return filtered
}
