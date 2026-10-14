package web

import (
	"errors"
	"fmt"

	"fluxport/internal/collections"
	"fluxport/internal/jobs"
	"github.com/gofiber/fiber/v3"
)

func (s *Server) prepareCleanedResult(c fiber.Ctx) error {
	owner := c.Params("id")
	var job jobs.Job
	var err error
	if retry := c.FormValue("retry"); retry != "" {
		job, err = s.Service.Jobs.Get(retry)
		if err == nil && (job.Kind != "prepare_result" || job.Resource != owner) {
			err = fmt.Errorf("cleaned result does not belong to this collection")
		}
		if err == nil {
			job, err = s.Service.RetryJob(retry)
		}
	} else {
		var definition collections.Pipeline
		definition, err = collections.DecodePipeline([]byte(c.FormValue("definition")))
		if err == nil {
			definition.Source = owner
			var source collections.Collection
			source, err = s.Service.Get(owner)
			if expected := c.FormValue("source_generation"); err == nil && expected != "" && source.Generation != expected {
				err = fmt.Errorf("source changed; capture the current input before preparing again: %w", collections.ErrConflict)
			}
			if err == nil {
				job, err = s.Service.PrepareResult(definition)
			}
		}
	}
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	if c.Get("HX-Request") == "true" {
		return s.renderCleanedResult(c, owner, job.ID)
	}
	return c.Redirect().Status(303).To("/collections/" + owner + "?result_id=" + job.ID)
}

func (s *Server) cleanedResultStatus(c fiber.Ctx) error {
	if c.Get("HX-Request") != "true" {
		return c.Redirect().Status(303).To("/collections/" + c.Params("id") + "?result_id=" + c.Params("result"))
	}
	return s.renderCleanedResult(c, c.Params("id"), c.Params("result"))
}

func (s *Server) resultPage(c fiber.Ctx, owner, resultID string) (Page, error) {
	p := s.base(c, "Cleaned data", "collections")
	p.Tab = "preview"
	p.ResultID = resultID
	var err error
	p.Collection, err = s.Service.Get(owner)
	if err != nil {
		return p, err
	}
	j, err := s.Service.Jobs.Get(resultID)
	if err != nil || j.Kind != "prepare_result" || j.Resource != owner {
		return p, fiber.NewError(404, "Cleaned result not found for this collection")
	}
	p.PreparedJob = &j
	if j.State == "ready" {
		d, e := s.Service.PreparedResult(owner, resultID)
		if e != nil {
			p.Message = e.Error()
			return p, nil
		}
		p.Prepared = &d
		c.Locals("result_id", resultID)
		if err = s.fillWorkbench(c, &p, p.Collection); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (s *Server) renderCleanedResult(c fiber.Ctx, owner, resultID string) error {
	p, err := s.resultPage(c, owner, resultID)
	if err != nil {
		return err
	}
	return c.Render("partials/prepared-result", p)
}

func rejectResultMutation(c fiber.Ctx) error {
	if c.FormValue("result_id") != "" || c.Query("result_id") != "" {
		return fiber.NewError(403, "Cleaned results are read-only. Switch to Original to edit the source.")
	}
	return nil
}

func resultHTTPError(err error) error {
	if errors.Is(err, collections.ErrPending) {
		return fiber.NewError(503, err.Error())
	}
	if errors.Is(err, collections.ErrConflict) {
		return fiber.NewError(409, err.Error())
	}
	return fiber.NewError(422, err.Error())
}
