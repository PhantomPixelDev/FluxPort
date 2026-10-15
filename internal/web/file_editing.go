package web

import (
	"fmt"
	"strconv"

	"github.com/gofiber/fiber/v3"
)

func (s *Server) fileHistory(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	history, err := s.Service.FileHistory(col.ID)
	if err != nil {
		return err
	}
	if c.Query("format") == "json" {
		state := fiber.Map{"history": history, "generation": col.Generation, "status": col.Status, "editable": !col.ReadOnly && col.Rows <= 100000, "error": col.Error}
		if jobID := c.Query("job_id"); jobID != "" {
			job, e := s.Service.Jobs.Get(jobID)
			if e != nil || job.Resource != col.ID || job.Kind != "file_cleanup" {
				return fiber.NewError(404, "File change not found")
			}
			state["job_state"], state["job_error"] = job.State, job.Error
		}
		return c.JSON(state)
	}
	p := s.base(c, col.Name, "collections")
	p.Collection, p.FileHistory = col, &history
	return s.renderCollectionView(c, "revisions", p)
}
func (s *Server) restoreFile(c fiber.Ctx) error {
	current, err := strconv.ParseInt(c.FormValue("current_revision"), 10, 64)
	if err != nil {
		return fiber.NewError(400, "Reload file history before restoring a version")
	}
	revision, _ := strconv.ParseInt(c.FormValue("revision"), 10, 64)
	if err = s.Service.RestoreFile(c.Params("id"), c.FormValue("generation"), c.FormValue("action"), revision, current); err != nil {
		if c.Get("Accept") == "application/json" {
			return fiber.NewError(409, err.Error())
		}
		p := s.base(c, "File history", "collections")
		p.Collection, _ = s.Service.Get(c.Params("id"))
		h, e := s.Service.FileHistory(p.Collection.ID)
		if e != nil {
			return e
		}
		p.FileHistory = &h
		p.Message = err.Error()
		c.Status(409)
		return s.renderCollectionView(c, "revisions", p)
	}
	if c.Get("Accept") == "application/json" {
		col, e := s.Service.Get(c.Params("id"))
		if e != nil {
			return e
		}
		h, e := s.Service.FileHistory(col.ID)
		if e != nil {
			return e
		}
		return c.JSON(fiber.Map{"history": h, "generation": col.Generation, "status": col.Status, "saved": true})
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id"))
}
func (s *Server) renderFileEditResponse(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	h, err := s.Service.FileHistory(col.ID)
	if err != nil {
		return err
	}
	if c.Get("Accept") == "application/json" {
		return c.JSON(fiber.Map{"history": h, "generation": col.Generation, "status": col.Status, "saved": true})
	}
	return c.Redirect().Status(303).To("/collections/" + col.ID)
}
func (s *Server) fileEditError(c fiber.Ctx, err error) error {
	if c.Get("Accept") == "application/json" {
		return fiber.NewError(409, fmt.Sprintf("Changes were not saved: %s", err.Error()))
	}
	return s.workbenchFailure(c, err)
}
