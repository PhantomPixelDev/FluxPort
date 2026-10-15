package web

import "github.com/gofiber/fiber/v3"

// Workspace fragments and native pages share the same view components.
func detailWorkspaceView(tab string) string {
	switch tab {
	case "downloads":
		return "export"
	case "schema":
		return "schema"
	case "raw":
		return "raw"
	case "configuration":
		return "api"
	default:
		return "data"
	}
}
func (s *Server) renderCollectionView(c fiber.Ctx, view string, p Page) error {
	p.WorkspaceView = view
	if view == "api" {
		p.APIOptions = s.APIPolicy.Global()
		p.APICollection = s.APIPolicy.Collection(p.Collection.ID)
		if c.Query("api_saved") != "" {
			p.APISettingsMessage = c.Query("api_saved")
		}
	}
	p.Title, p.Active = p.Collection.Name, "collections"
	if view == "advanced" && p.Pipeline != nil {
		saved := p.Pipeline.Saved[:0]
		for _, recipe := range p.Pipeline.Saved {
			if recipe.Source == p.Collection.ID {
				saved = append(saved, recipe)
			}
		}
		p.Pipeline.Saved = saved
	}
	if p.FileHistory == nil {
		h, err := s.Service.FileHistory(p.Collection.ID)
		if err != nil {
			return err
		}
		p.FileHistory = &h
	}
	file, err := s.Service.Get(p.Collection.ID)
	if err != nil {
		return err
	}
	p.FileEditable = !file.ReadOnly && file.Rows <= 100000
	c.Vary("X-Fluxport-View")
	if c.Get("X-Fluxport-View") == "workspace" {
		return c.Render("partials/workspace-view", p)
	}
	source := p.Collection
	if p.ResultID != "" {
		if current, err := s.Service.Get(p.Collection.ID); err == nil {
			source = current
		}
	}
	original := s.base(c, source.Name, "collections")
	original.Collection, original.Tab = source, "preview"
	if view == "data" && p.Workbench != nil && p.ResultID == "" && p.Workbench.ResultID == "" {
		original = p
	} else {
		previousResult, previousQuery := c.Locals("result_id"), c.Locals("workbench_query")
		c.Locals("result_id", "")
		c.Locals("workbench_query", "{}")
		err := s.fillWorkbench(c, &original, source)
		c.Locals("result_id", previousResult)
		c.Locals("workbench_query", previousQuery)
		if err != nil {
			original.Message = "The file is not ready. Refresh when processing finishes."
		}
	}
	original.Original = nil
	p.Original = &original
	return c.Render("pages/workspace", p, "layouts/base")
}
