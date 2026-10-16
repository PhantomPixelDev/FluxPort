package api

import (
	"strings"

	"fluxport/internal/collections"
	"github.com/gofiber/fiber/v3"
)

func (a *API) pipelineAccess(p collections.Pipeline) error {
	_, ids, err := collections.CompilePipeline(p)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	for _, id := range ids {
		col, e := a.Service.Get(id)
		if e != nil {
			return fiber.NewError(404, "Pipeline collection not found.")
		}
		if !col.Enabled {
			return fiber.NewError(404, "Pipeline collection endpoint is disabled.")
		}
		if rules := strings.TrimSpace(col.Rules); rules != "" && rules != "{}" {
			return fiber.NewError(422, "Pipeline API requires collections without response transformations.")
		}
	}
	return nil
}
func (a *API) pipelines(c fiber.Ctx) error {
	items, err := a.Service.Pipelines()
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"pipelines": items})
}
func (a *API) savePipeline(c fiber.Ctx) error {
	p, err := collections.DecodePipeline(c.Body())
	if err != nil {
		return fiber.NewError(400, err.Error())
	}
	if err = a.pipelineAccess(p); err != nil {
		return err
	}
	p, err = a.Service.SavePipeline(p)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Status(201).JSON(p)
}
func (a *API) getPipeline(c fiber.Ctx) error {
	p, err := a.Service.GetPipeline(c.Params("pipeline"))
	if err != nil {
		return fiber.NewError(404, "Pipeline not found.")
	}
	return c.JSON(p)
}
func (a *API) runPipeline(c fiber.Ctx) error {
	p, err := a.Service.GetPipeline(c.Params("pipeline"))
	if err != nil {
		return fiber.NewError(404, "Pipeline not found.")
	}
	var body struct {
		Parameters map[string]any `json:"parameters,omitempty"`
		Source     string         `json:"source,omitempty"`
		Format     string         `json:"format,omitempty"`
		Through    *int           `json:"through,omitempty"`
	}
	if len(c.Body()) > 0 {
		if err = decodeRequest(c, &body); err != nil {
			return err
		}
	}
	if body.Source != "" {
		p.Source = body.Source
	}
	if body.Parameters != nil {
		p, err = collections.BindPipeline(p, body.Parameters)
		if err != nil {
			return fiber.NewError(422, err.Error())
		}
	}
	if err = a.pipelineAccess(p); err != nil {
		return err
	}
	if strings.HasSuffix(c.Path(), "/preview") {
		through := len(p.Steps) - 1
		if body.Through != nil {
			through = *body.Through
		}
		if through < 0 || through >= len(p.Steps) {
			if len(p.Steps) > 0 {
				return fiber.NewError(400, "Step index is outside the pipeline.")
			}
		}
		rows, e := a.Service.PreviewPipeline(c.Context(), p, through)
		if e != nil {
			return indexedError(c, e)
		}
		return c.JSON(fiber.Map{"records": rows, "limit": 20, "through": through})
	}
	format := body.Format
	if format == "" {
		format = "json"
	}
	job, err := a.Service.PipelineJob(p, format)
	if err != nil {
		return indexedError(c, err)
	}
	return c.Status(202).JSON(job)
}
func (a *API) downloadJob(c fiber.Ctx) error {
	path, err := a.Service.Download(c.Params("id"))
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Download(path)
}
