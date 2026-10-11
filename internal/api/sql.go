package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

func (a *API) runSQL(c fiber.Ctx, ids []string, text string) error {
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
	result, err := a.Service.SQL(c.Context(), ids, text)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(result)
}
func (a *API) sql(c fiber.Ctx) error {
	var body struct {
		Collections []string `json:"collections"`
		SQL         string   `json:"sql"`
	}
	if err := decodeRequest(c, &body); err != nil {
		return err
	}
	return a.runSQL(c, body.Collections, body.SQL)
}
func (a *API) savedSQL(c fiber.Ctx) error {
	q, err := a.Service.GetSQL(c.Params("query"))
	if err != nil || !q.Published {
		return fiber.NewError(404, "Published query not found.")
	}
	return a.runSQL(c, q.Collections, q.SQL)
}
