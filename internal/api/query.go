package api

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

func decodeRequest(c fiber.Ctx, target any) error {
	if len(c.Body()) > 1<<20 {
		return fiber.NewError(413, "Request exceeds 1 MiB.")
	}
	// Use the strict format decoder first: ambiguous duplicate keys are rejected.
	if _, err := conversion.Decode(c.Body(), "json"); err != nil {
		return fiber.NewError(400, err.Error())
	}
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fiber.NewError(400, "Invalid JSON request: "+err.Error())
	}
	return nil
}
func (a *API) query(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	if rules := strings.TrimSpace(col.Rules); rules != "" && rules != "{}" {
		return fiber.NewError(422, "Advanced source queries require an endpoint without transformations.")
	}
	var q collections.Query
	if err = decodeRequest(c, &q); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	result, err := a.Service.Query(ctx, col, q)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(result)
}
func (a *API) bulk(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	var body struct {
		Generation string                 `json:"generation"`
		Changes    []collections.Mutation `json:"changes"`
	}
	if err = decodeRequest(c, &body); err != nil {
		return err
	}
	result, err := a.Service.BulkMutate(col.ID, body.Generation, body.Changes)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(fiber.Map{"results": result})
}
func (a *API) views(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	views, err := a.Service.Views(col.ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"views": views})
}
func (a *API) saveView(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	var view collections.View
	if err = decodeRequest(c, &view); err != nil {
		return err
	}
	view.CollectionID = col.ID
	view, err = a.Service.SaveView(view)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Status(201).JSON(view)
}
func (a *API) deleteView(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	if err = a.Service.DeleteView(col.ID, c.Params("view")); err != nil {
		return err
	}
	return c.SendStatus(204)
}
