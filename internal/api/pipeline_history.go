package api

import (
	"fluxport/internal/collections"
	"github.com/gofiber/fiber/v3"
)

func (a *API) pipelineRevisions(c fiber.Ctx) error {
	if _, err := a.Service.GetPipeline(c.Params("pipeline")); err != nil {
		return indexedError(c, err)
	}
	revisions, err := a.Service.PipelineRevisions(c.Params("pipeline"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"revisions": revisions})
}
func (a *API) pipelineRuns(c fiber.Ctx) error {
	runs, err := a.Service.PipelineRuns(c.Params("pipeline"))
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(fiber.Map{"runs": runs})
}
func (a *API) restorePipeline(c fiber.Ctx) error {
	var request struct {
		Revision int `json:"revision"`
	}
	if err := decodeRequest(c, &request); err != nil {
		return err
	}
	p, err := a.Service.RestorePipeline(c.Params("pipeline"), request.Revision)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(p)
}

func (a *API) getSchedule(c fiber.Ctx) error {
	if _, err := a.Service.GetPipeline(c.Params("pipeline")); err != nil {
		return indexedError(c, err)
	}
	schedule, err := a.Service.Schedule(c.Params("pipeline"))
	if err != nil {
		return err
	}
	return c.JSON(schedule)
}
func (a *API) saveSchedule(c fiber.Ctx) error {
	var schedule collections.Schedule
	if err := decodeRequest(c, &schedule); err != nil {
		return err
	}
	schedule.Pipeline = c.Params("pipeline")
	if err := a.Service.SaveSchedule(schedule); err != nil {
		return indexedError(c, err)
	}
	return c.SendStatus(204)
}
