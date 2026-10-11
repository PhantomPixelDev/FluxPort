// Package api owns public REST endpoints; dashboard handlers live in web.
package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"sort"
	"strconv"
	"strings"
	"time"

	"fluxport/internal/auth"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/transformations"
	"github.com/gofiber/fiber/v3"
)

type API struct {
	Service *collections.Service
	Key     string
}

func (a *API) Register(app *fiber.App) {
	g := app.Group("/api", auth.API(a.Key))
	g.Get("/jobs", func(c fiber.Ctx) error {
		jobs, err := a.Service.Jobs.List()
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"jobs": jobs})
	})
	g.Get("/jobs/:id", func(c fiber.Ctx) error {
		job, err := a.Service.Jobs.Get(c.Params("id"))
		if err != nil {
			return err
		}
		return c.JSON(job)
	})
	g.Post("/jobs/:id/cancel", func(c fiber.Ctx) error {
		if err := a.Service.Jobs.Cancel(c.Params("id")); err != nil {
			return err
		}
		return c.SendStatus(204)
	})
	g.Post("/jobs/:id/retry", func(c fiber.Ctx) error {
		job, err := a.Service.RetryJob(c.Params("id"))
		if err != nil {
			return fiber.NewError(422, err.Error())
		}
		return c.Status(202).JSON(job)
	})
	g.Get("/collections", func(c fiber.Ctx) error {
		p, err := integer(c, "page", 1, 1, 1000000)
		if err != nil {
			return err
		}
		limit, err := integer(c, "limit", 20, 1, 100)
		if err != nil {
			return err
		}
		items, total, err := a.Service.List(c.Query("q"), p, limit)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"collections": items, "total": total, "page": p, "limit": limit})
	})
	g.Get("/openapi.json", a.openapi)
	g.Post("/sql", a.sql)
	g.Get("/queries/:query", a.savedSQL)
	g.Get("/collections/:id/schema", a.schema)
	g.Post("/collections/:id/query", a.query)
	g.Post("/collections/:id/bulk", a.bulk)
	g.Get("/collections/:id/views", a.views)
	g.Post("/collections/:id/views", a.saveView)
	g.Delete("/collections/:id/views/:view", a.deleteView)
	g.Get("/collections/:id", a.read)
	g.Get("/collections/:id/rows/:row", a.readRow)
	g.Post("/collections/:id/rows", a.mutate)
	g.Put("/collections/:id/rows/:row", a.mutate)
	g.Patch("/collections/:id/rows/:row", a.mutate)
	g.Delete("/collections/:id/rows/:row", a.mutate)
}
func integer(c fiber.Ctx, key string, def, min, max int) (int, error) {
	s := c.Query(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fiber.NewError(400, fmt.Sprintf("%s must be between %d and %d", key, min, max))
	}
	return n, nil
}
func (a *API) collection(c fiber.Ctx) (collections.Collection, error) {
	col, err := a.Service.Get(c.Params("id"))
	if errors.Is(err, sql.ErrNoRows) {
		return col, fiber.NewError(404, "Collection not found.")
	}
	if err != nil {
		return col, err
	}
	if !col.Enabled {
		return col, fiber.NewError(404, "Endpoint is disabled.")
	}
	return col, nil
}
func negotiate(c fiber.Ctx) (string, error) {
	if f := c.Query("format"); f != "" {
		f = conversion.Format(f)
		if !conversion.Supported(f) {
			return "", fiber.NewError(400, "Unsupported output format.")
		}
		return f, nil
	}
	accept := c.Get("Accept")
	if accept == "" {
		return "json", nil
	}
	// The most specific media range controls each representation, including q=0.
	best := ""
	bestQ := 0.0
	for _, f := range conversion.Formats {
		m := conversion.MIME(f)
		quality := 0.0
		specificity := -1
		for _, part := range strings.Split(accept, ",") {
			mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil {
				continue
			}
			q := 1.0
			if value, ok := params["q"]; ok {
				q, err = strconv.ParseFloat(value, 64)
				if err != nil || !(q >= 0 && q <= 1) {
					continue
				}
			}
			spec := -1
			if mt == "*/*" {
				spec = 0
			} else if mt == strings.Split(m, "/")[0]+"/*" {
				spec = 1
			} else if mt == m || (f == "yaml" && (mt == "text/yaml" || mt == "application/x-yaml")) {
				spec = 2
			}
			if spec > specificity || (spec == specificity && q > quality) {
				specificity = spec
				quality = q
			}
		}
		if specificity >= 0 && quality > bestQ {
			best = f
			bestQ = quality
		}
	}
	if best == "" {
		return "", fiber.NewError(406, "No supported response format in Accept header.")
	}
	return best, nil
}
func send(c fiber.Ctx, v any) error {
	f, err := negotiate(c)
	if err != nil {
		return err
	}
	b, err := conversion.Encode(v, f)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	c.Set("Content-Type", conversion.MIME(f)+"; charset=utf-8")
	c.Vary("Accept")
	return c.Send(b)
}
func indexedError(c fiber.Ctx, err error) error {
	if errors.Is(err, collections.ErrConflict) {
		return fiber.NewError(409, err.Error())
	}
	if errors.Is(err, collections.ErrPending) {
		c.Set("Retry-After", "2")
		return fiber.NewError(503, err.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fiber.NewError(504, "Filtered scan timed out; try a narrower query.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(404, "Collection not found.")
	}
	return fiber.NewError(422, err.Error())
}
func (a *API) read(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	p, err := integer(c, "page", 1, 1, 1000000000)
	if err != nil {
		return err
	}
	limit, err := integer(c, "limit", 50, 1, 100)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	rows, total, err := a.Service.Page(ctx, col, p, limit, true, c.Query("q"))
	if err != nil {
		return indexedError(c, err)
	}
	c.Set("X-Total-Count", strconv.FormatInt(total, 10))
	c.Set("X-Page", strconv.Itoa(p))
	return send(c, rows)
}
func (a *API) readRow(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(c.Params("row"))
	if err != nil || n < 0 || n >= 1000000000 {
		return fiber.NewError(404, "Row not found.")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	rows, _, err := a.Service.Page(ctx, col, n+1, 1, true, c.Query("q"))
	if err != nil {
		return indexedError(c, err)
	}
	if len(rows) == 0 {
		return fiber.NewError(404, "Row not found.")
	}
	return send(c, rows[0])
}
func (a *API) schema(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	fields, err := a.Service.Fields(col)
	if err != nil {
		return indexedError(c, err)
	}
	total := col.Rows
	if col.Rules != "" || c.Query("q") != "" {
		ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
		defer cancel()
		rules, e := transformations.Parse(col.Rules)
		if e != nil {
			return e
		}
		found := map[string]collections.Field{}
		total = 0
		err = a.Service.Each(ctx, col, func(v any) error {
			rows := transformations.Apply([]any{v}, rules)
			if len(rows) == 0 {
				return nil
			}
			if q := strings.ToLower(c.Query("q")); q != "" {
				b, _ := conversion.Encode(rows[0], "json")
				if !strings.Contains(strings.ToLower(string(b)), q) {
					return nil
				}
			}
			total++
			for _, field := range collections.Schema(rows) {
				if _, known := found[field.Name]; !known && len(found) >= 2048 {
					return fmt.Errorf("schema field limit exceeded")
				}
				if prev, ok := found[field.Name]; ok {
					types := map[string]bool{}
					for _, typ := range strings.Split(prev.Types+", "+field.Types, ", ") {
						types[typ] = true
					}
					names := []string{}
					for typ := range types {
						names = append(names, typ)
					}
					sort.Strings(names)
					field.Types = strings.Join(names, ", ")
				}
				found[field.Name] = field
			}
			return nil
		})
		if err != nil {
			return indexedError(c, err)
		}
		fields = []collections.Field{}
		for _, field := range found {
			fields = append(fields, field)
		}
		sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	}
	return c.JSON(fiber.Map{"collection": col.ID, "fields": fields, "rows": total})
}
func (a *API) mutate(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	if !col.CRUD {
		return fiber.NewError(405, "CRUD is disabled for this collection.")
	}
	n := 0
	if c.Method() != "POST" {
		n, err = strconv.Atoi(c.Params("row"))
		if err != nil || n < 0 {
			return fiber.NewError(400, "Row must be a non-negative index.")
		}
	}
	var value any
	if c.Method() != "DELETE" {
		mt, _, _ := mime.ParseMediaType(c.Get("Content-Type"))
		if mt != "application/json" {
			return fiber.NewError(415, "CRUD request bodies must use application/json.")
		}
		value, err = conversion.Decode(c.Body(), "json")
		if err != nil {
			return fiber.NewError(400, err.Error())
		}
	}
	result, err := a.Service.Mutate(col.ID, c.Method(), n, value)
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(404, "Row not found.")
	}
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	if c.Method() == "DELETE" {
		return c.SendStatus(204)
	}
	if c.Method() == "POST" {
		c.Status(201)
	}
	return c.JSON(result)
}
func (a *API) openapi(c fiber.Ctx) error {
	paths := map[string]any{}
	queryBody := fiber.Map{"required": true, "content": fiber.Map{"application/json": fiber.Map{"schema": fiber.Map{"type": "object", "required": []string{"collections", "sql"}, "properties": fiber.Map{"collections": fiber.Map{"type": "array", "minItems": 1, "maxItems": 8, "items": fiber.Map{"type": "string"}}, "sql": fiber.Map{"type": "string", "maxLength": 16384}}}}}}
	paths["/api/sql"] = fiber.Map{"post": fiber.Map{"summary": "Run an isolated read-only SQL query on selected collections", "requestBody": queryBody, "responses": fiber.Map{"200": fiber.Map{"description": "Columns, rows, duration and truncation status"}, "422": fiber.Map{"description": "Invalid, unsafe or resource-limited query"}, "504": fiber.Map{"description": "Query timed out"}}}}
	paths["/api/queries/{query}"] = fiber.Map{"get": fiber.Map{"summary": "Execute a published saved query", "parameters": []any{fiber.Map{"name": "query", "in": "path", "required": true, "schema": fiber.Map{"type": "string"}}}, "responses": fiber.Map{"200": fiber.Map{"description": "Read-only SQL results"}, "404": fiber.Map{"description": "Published query not found"}}}}
	paths["/api/jobs"] = fiber.Map{"get": fiber.Map{"summary": "List recent background jobs", "responses": fiber.Map{"200": fiber.Map{"description": "Job states and progress"}}}}
	jobParam := []any{fiber.Map{"name": "id", "in": "path", "required": true, "schema": fiber.Map{"type": "string"}}}
	paths["/api/jobs/{id}"] = fiber.Map{"get": fiber.Map{"summary": "Read background job status", "parameters": jobParam, "responses": fiber.Map{"200": fiber.Map{"description": "Job status"}}}}
	for _, action := range []string{"cancel", "retry"} {
		paths["/api/jobs/{id}/"+action] = fiber.Map{"post": fiber.Map{"summary": action + " background job", "parameters": jobParam, "responses": fiber.Map{"202": fiber.Map{"description": "Retry queued"}, "204": fiber.Map{"description": "Cancellation requested"}}}}
	}
	page := 1
	for {
		items, total, err := a.Service.List("", page, 100)
		if err != nil {
			return err
		}
		for _, col := range items {
			if !col.Enabled {
				continue
			}
			base := "/api/collections/" + col.ID
			paths[base+"/query"] = fiber.Map{"post": fiber.Map{"summary": "Filter and sort indexed source records with stable IDs", "requestBody": fiber.Map{"required": true, "content": fiber.Map{"application/json": fiber.Map{"schema": fiber.Map{"type": "object", "properties": fiber.Map{"search": fiber.Map{"type": "string", "maxLength": 256}, "filters": fiber.Map{"type": "array", "maxItems": 20, "items": fiber.Map{"type": "object"}}, "sort": fiber.Map{"type": "array", "maxItems": 8, "items": fiber.Map{"type": "object"}}, "page": fiber.Map{"type": "integer", "minimum": 1}, "limit": fiber.Map{"type": "integer", "minimum": 1, "maximum": 100}}}}}}, "responses": fiber.Map{"200": fiber.Map{"description": "Records, total, page, limit and index generation"}, "503": fiber.Map{"description": "Indexing pending"}}}}
			if col.CRUD && !col.ReadOnly {
				paths[base+"/bulk"] = fiber.Map{"post": fiber.Map{"summary": "Atomically edit up to 1000 stable record IDs; requires generation", "requestBody": fiber.Map{"required": true, "content": fiber.Map{"application/json": fiber.Map{"schema": fiber.Map{"type": "object", "required": []string{"generation", "changes"}}}}}, "responses": fiber.Map{"200": fiber.Map{"description": "Batch committed"}, "409": fiber.Map{"description": "Source changed; reload before editing"}, "422": fiber.Map{"description": "Batch rejected without changing the source"}}}}
			}
			paths[base] = fiber.Map{"get": fiber.Map{"summary": "Read " + col.Name, "parameters": []any{fiber.Map{"name": "page", "in": "query", "schema": fiber.Map{"type": "integer", "minimum": 1}}, fiber.Map{"name": "limit", "in": "query", "schema": fiber.Map{"type": "integer", "minimum": 1, "maximum": 100}}, fiber.Map{"name": "format", "in": "query", "schema": fiber.Map{"type": "string", "enum": conversion.Formats}}, fiber.Map{"name": "q", "in": "query", "schema": fiber.Map{"type": "string"}}}, "responses": fiber.Map{"200": fiber.Map{"description": "Paginated collection data"}, "503": fiber.Map{"description": "Source is being indexed; Retry-After: 2"}}}}
			paths[base+"/schema"] = fiber.Map{"get": fiber.Map{"summary": "Inspect inferred schema", "responses": fiber.Map{"200": fiber.Map{"description": "Field names and observed types"}}}}
			rowParam := []any{fiber.Map{"name": "row", "in": "path", "required": true, "schema": fiber.Map{"type": "integer", "minimum": 0}}}
			rowOps := fiber.Map{"get": fiber.Map{"summary": "Read row by zero-based index", "parameters": rowParam, "responses": fiber.Map{"200": fiber.Map{"description": "Row"}}}}
			if col.CRUD {
				body := fiber.Map{"required": true, "content": fiber.Map{"application/json": fiber.Map{"schema": fiber.Map{}}}}
				paths[base+"/rows"] = fiber.Map{"post": fiber.Map{"summary": "Append row", "requestBody": body, "responses": fiber.Map{"201": fiber.Map{"description": "Created"}}}}
				for _, method := range []string{"put", "patch", "delete"} {
					op := fiber.Map{"summary": "Modify source row", "parameters": rowParam, "responses": fiber.Map{"200": fiber.Map{"description": "Updated"}, "204": fiber.Map{"description": "Deleted"}}}
					if method != "delete" {
						op["requestBody"] = body
					}
					rowOps[method] = op
				}
			}
			paths[base+"/rows/{row}"] = rowOps
		}
		if page*100 >= total {
			break
		}
		page++
	}
	doc := fiber.Map{"openapi": "3.0.3", "info": fiber.Map{"title": "FluxPort API", "version": "1.0.0"}, "paths": paths}
	if a.Key != "" {
		doc["components"] = fiber.Map{"securitySchemes": fiber.Map{"bearerAuth": fiber.Map{"type": "http", "scheme": "bearer"}}}
		doc["security"] = []any{fiber.Map{"bearerAuth": []string{}}}
	}
	return c.JSON(doc)
}
