package web

import (
	"errors"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
	"os"
)

func (s *Server) fillConverter(c fiber.Ctx, p *Page) error {
	var err error
	p.Collections, p.Count, err = s.Service.List("", 1, 100)
	if err != nil {
		return err
	}
	p.InputMode = c.FormValue("input_mode", "input")
	p.CollectionID = c.Query("collection", c.FormValue("collection_id"))
	if id, ok := c.Locals("converter_collection").(string); ok {
		p.CollectionID = id
	}
	if p.CollectionID != "" {
		p.Collection, err = s.Service.Get(p.CollectionID)
		if err != nil {
			return err
		}
		p.InputMode, p.InputFormat = "collection", p.Collection.Format
		found := false
		for _, col := range p.Collections {
			found = found || col.ID == p.CollectionID
		}
		if !found {
			p.Collections = append(p.Collections, p.Collection)
		}
	}
	return nil
}

func (s *Server) converter(c fiber.Ctx) error {
	p := s.base(c, "Convert data", "converter")
	if err := s.fillConverter(c, &p); err != nil {
		return err
	}
	return c.Render("pages/converter", p, "layouts/base")
}

func (s *Server) converterCollections(c fiber.Ctx) error {
	p := s.base(c, "Choose a collection", "converter")
	p.Query = c.Query("collection_search")
	if len(p.Query) > 256 {
		return fiber.NewError(400, "Search is limited to 256 characters.")
	}
	var err error
	p.Collections, p.Count, err = s.Service.List(p.Query, 1, 100)
	if err != nil {
		return err
	}
	p.CollectionID = c.Query("collection_id")
	return c.Render("partials/converter-collections", p)
}

func (s *Server) convertCollection(c fiber.Ctx, id, to string) error {
	rules := c.FormValue("column_types")
	if value, ok := c.Locals("column_rules").(string); ok {
		rules = value
	}
	if _, err := conversion.ParseColumnTypes(rules); err != nil {
		return fiber.NewError(422, err.Error())
	}
	if id == "" {
		return fiber.NewError(400, "Choose a collection to convert.")
	}
	to = conversion.Format(defaultFormat(to, "yaml"))
	if !conversion.Supported(to) {
		return fiber.NewError(422, "Choose a supported output format.")
	}
	selected, err := s.Service.Get(id)
	if err != nil {
		return err
	}
	if selected.Format == "xlsx" || selected.Format == "parquet" {
		if _, err = s.Service.ExportJobWithOptions(id, to, converterOptions(c)); err != nil {
			return fiber.NewError(422, err.Error())
		}
		if c.Get("HX-Request") != "true" {
			return c.Redirect().Status(303).To("/converter?collection=" + id + "#activity")
		}
		c.Set("HX-Trigger", "jobQueued")
		p := s.base(c, "Convert data", "converter")
		p.Message = "Binary dataset conversion queued. Download the result in File activity. Excel cells retain their imported text values; Parquet dates use text values."
		return c.Render("partials/queued", p)
	}
	path, col, err := s.Service.ConversionInput(id)
	if err != nil {
		if errors.Is(err, collections.ErrPending) {
			c.Set("Retry-After", "2")
			return fiber.NewError(503, "This collection is being indexed. Try again when it is ready.")
		}
		return fiber.NewError(422, err.Error())
	}
	keep := false
	defer func() {
		if !keep {
			os.Remove(path)
		}
	}()
	c.Locals("converter_collection", id)
	if col.Size <= s.Service.MaxBytes {
		input, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return s.convertValues(c, input, col.Format, to)
	}
	if _, err = s.Service.ConvertJobWithOptions(path, col.Format, to, converterOptions(c)); err != nil {
		return fiber.NewError(422, err.Error())
	}
	keep = true
	if c.Get("HX-Request") != "true" {
		return c.Redirect().Status(303).To("/converter?collection=" + id + "#activity")
	}
	c.Set("HX-Trigger", "jobQueued")
	p := s.base(c, "Convert data", "converter")
	p.Message = "Converting " + col.Name + " in the background. Follow File activity, then download the complete result. Your source stays unchanged. Large conversions do not run round-trip checks."
	return c.Render("partials/queued", p)
}

func (s *Server) workflow(c fiber.Ctx) error {
	p := s.base(c, "Data workflow", "workflow")
	p.Query = c.Query("q")
	if len(p.Query) > 256 {
		return fiber.NewError(400, "Search is limited to 256 characters.")
	}
	var err error
	p.Collections, p.Count, err = s.Service.List(p.Query, 1, 100)
	if err != nil {
		return err
	}
	p.CollectionID = c.Query("collection")
	if p.CollectionID != "" {
		p.Collection, err = s.Service.Get(p.CollectionID)
		if err != nil {
			return err
		}
	}
	return c.Render("pages/workflow", p, "layouts/base")
}
