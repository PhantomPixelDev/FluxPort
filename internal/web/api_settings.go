package web

import (
	"fmt"
	"strconv"
	"strings"

	"fluxport/internal/apipolicy"
	"github.com/gofiber/fiber/v3"
)

func policyNumber(c fiber.Ctx, name string, fallback int) (int, error) {
	value := c.FormValue(name)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", strings.ReplaceAll(name, "_", " "))
	}
	return n, nil
}
func policyForm(c fiber.Ctx, rate *apipolicy.Rate, def, max *int) error {
	rate.Enabled = c.FormValue("rate_enabled") == "on"
	for _, field := range []struct {
		name   string
		target *int
	}{{"read_limit", &rate.Reads}, {"write_limit", &rate.Writes}, {"window_seconds", &rate.WindowSeconds}, {"default_page_size", def}, {"max_page_size", max}} {
		value, err := policyNumber(c, field.name, *field.target)
		if err != nil {
			return err
		}
		*field.target = value
	}
	return nil
}
func (s *Server) saveAPISettings(c fiber.Ctx) error {
	p := s.APIPolicy.Global()
	if err := policyForm(c, &p.Rate, &p.DefaultPageSize, &p.MaxPageSize); err != nil {
		return fiber.NewError(400, err.Error())
	}
	p.Origins = strings.Fields(c.FormValue("origins"))
	if err := s.APIPolicy.SaveGlobal(p); err != nil {
		return fiber.NewError(400, err.Error())
	}
	return c.Redirect().Status(303).To("/settings?api_saved=1#api-settings")
}
func (s *Server) saveCollectionAPISettings(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	options := s.APIPolicy.Collection(col.ID)
	if err = policyForm(c, &options.Rate, &options.DefaultPageSize, &options.MaxPageSize); err != nil {
		return fiber.NewError(400, err.Error())
	}
	if err = s.APIPolicy.SaveCollection(col.ID, options); err != nil {
		return fiber.NewError(400, err.Error())
	}
	if c.Get("X-Fluxport-View") == "workspace" {
		p := s.base(c, col.Name, "collections")
		p.Collection = col
		p.Tab = "configuration"
		p.APISettingsMessage = "1"
		return s.renderCollectionView(c, "api", p)
	}
	return c.Redirect().Status(303).To("/collections/" + col.ID + "?tab=configuration&api_saved=1")
}
