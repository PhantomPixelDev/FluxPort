package api

import (
	"context"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/quality"
	"github.com/gofiber/fiber/v3"
)

func (a *API) profile(c fiber.Ctx) error {
	limit, err := integer(c, "limit", 10000, 1, 100000)
	if err != nil {
		return err
	}
	return a.quality(c, collections.QualityRequest{Limit: limit})
}
func (a *API) validate(c fiber.Ctx) error {
	var request collections.QualityRequest
	if err := decodeRequest(c, &request); err != nil {
		return err
	}
	if request.Rules == nil {
		var err error
		request.Rules, err = a.Service.ValidationRules(c.Params("id"))
		if err != nil {
			return err
		}
	}
	return a.quality(c, request)
}
func (a *API) quality(c fiber.Ctx, request collections.QualityRequest) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	report, err := a.Service.Quality(ctx, col, request)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(report)
}
func (a *API) validationRules(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	rules, err := a.Service.ValidationRules(col.ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"rules": rules})
}
func (a *API) saveValidationRules(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	var request struct {
		Rules []quality.Rule `json:"rules"`
	}
	if err = decodeRequest(c, &request); err != nil {
		return err
	}
	if err = a.Service.SaveValidationRules(col.ID, request.Rules); err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.SendStatus(204)
}
