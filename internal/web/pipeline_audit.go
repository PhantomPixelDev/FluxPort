package web

import "github.com/gofiber/fiber/v3"

func (s *Server) pipelineAudit(c fiber.Ctx) error {
	p, err := s.Service.GetPipeline(c.Params("pipeline"))
	if err != nil {
		return fiber.NewError(404, "Saved recipe not found.")
	}
	j, err := s.Service.PipelineAuditJob(p)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Redirect().Status(303).To("/collections/" + p.Source + "?tab=configuration&audit_job=" + j.ID + "#activity")
}
