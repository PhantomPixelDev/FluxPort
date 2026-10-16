package api

import (
	"fluxport/internal/collections"
	"strings"

	"github.com/gofiber/fiber/v3"
)

func (a *API) runSQL(c fiber.Ctx, ids []string, text, engine string) error {
	for _, id := range ids {
		col, err := a.Service.Get(id)
		if err != nil {
			return indexedError(c, err)
		}
		if !col.Enabled {
			return fiber.NewError(404, "Collection endpoint is disabled.")
		}
		if rules := strings.TrimSpace(col.Rules); rules != "" && rules != "{}" {
			return fiber.NewError(422, "SQL API requires collections without response transformations.")
		}
	}
	result, err := a.Service.SQLWithEngine(c.Context(), ids, text, engine)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(result)
}
func (a *API) sql(c fiber.Ctx) error {
	var body struct {
		Parameters  map[string]any `json:"parameters,omitempty"`
		Explain     bool           `json:"explain,omitempty"`
		Collections []string       `json:"collections"`
		SQL         string         `json:"sql"`
		Engine      string         `json:"engine"`
	}
	if err := decodeRequest(c, &body); err != nil {
		return err
	}
	text, err := collections.BindSQL(body.SQL, body.Parameters)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	if body.Explain {
		if err = collections.ValidateSQL(text); err != nil {
			return fiber.NewError(422, err.Error())
		}
		if body.Engine == "native" {
			text = "EXPLAIN QUERY PLAN " + text
		} else {
			body.Engine = "duckdb"
			text = "EXPLAIN " + text
		}
	}
	return a.runSQL(c, body.Collections, text, body.Engine)
}
func (a *API) savedSQL(c fiber.Ctx) error {
	q, err := a.Service.GetSQL(c.Params("query"))
	if err != nil || !q.Published {
		return fiber.NewError(404, "Published query not found.")
	}
	return a.runSQL(c, q.Collections, q.SQL, "auto")
}
