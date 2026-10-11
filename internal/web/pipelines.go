package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/quality"
	"github.com/gofiber/fiber/v3"
)

type PipelineCard struct {
	Op, Definition, ID string
	Parameters         PipelineParameters
}
type PipelinePage struct {
	Schedule       collections.Schedule
	ValidationJSON string
	Revisions      []collections.PipelineRevision
	Runs           []jobs.Job
	Definition     collections.Pipeline
	Saved          []collections.Pipeline
	Cards          []PipelineCard
	Preview        string
	Through        int
	SourceQuery    string
	AvailableCount int
	HasPreview     bool
	PreviewStep    int
	ImportText     string
}

func decodePipelineSteps(values [][]byte) ([]collections.PipelineStep, error) {
	if len(values) > 20 {
		return nil, fmt.Errorf("pipelines support at most 20 steps")
	}
	steps := []collections.PipelineStep{}
	for i, data := range values {
		if len(data) > 8192 {
			return nil, fmt.Errorf("step %d exceeds 8 KiB", i+1)
		}
		if _, err := conversion.Decode(data, "json"); err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		var step collections.PipelineStep
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&step); err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
func (s *Server) pipelines(c fiber.Ctx) error {
	p := s.base(c, "Pipelines", "pipelines")
	pp := &PipelinePage{Definition: collections.Pipeline{Version: 1}, Through: -1}
	p.Pipeline = pp
	var err error
	pp.Saved, err = s.Service.Pipelines()
	if err != nil {
		return err
	}
	if id := c.Query("saved"); id != "" {
		pp.Definition, err = s.Service.GetPipeline(id)
		if err != nil {
			return fiber.NewError(404, "Pipeline not found.")
		}
	}
	pp.SourceQuery = c.Query("q")
	if c.Query("collection") != "" {
		pp.Definition.Source = c.Query("collection")
	}
	if c.Method() == "POST" {
		pp.ValidationJSON = c.FormValue("validation", "[]")
		pp.ImportText = c.FormValue("definition")
		pp.Definition.Name = strings.TrimSpace(c.FormValue("name"))
		pp.Definition.Source = c.FormValue("source")
		pp.Definition.Revision, _ = strconv.Atoi(c.FormValue("revision"))
		pp.SourceQuery = c.FormValue("q")
		posted := c.Request().PostArgs().PeekMulti("steps")
		pp.Definition.Steps, err = decodePipelineSteps(posted)
		if err == nil {
			text := c.FormValue("validation", "[]")
			var rules []quality.Rule
			if len(text) > 65536 {
				err = fmt.Errorf("validation rules exceed 64 KiB")
			} else if _, err = conversion.Decode([]byte(text), "json"); err == nil {
				d := json.NewDecoder(strings.NewReader(text))
				d.UseNumber()
				d.DisallowUnknownFields()
				err = d.Decode(&rules)
			}
			if err == nil {
				pp.Definition.Validation = rules
			}
		}
		if err == nil {
			err = applyPipelineParameters(c, pp.Definition.Steps)
		}
		if err != nil {
			for _, data := range posted {
				if len(pp.Cards) == 20 {
					break
				}
				var hint struct {
					Op string `json:"op"`
				}
				_ = json.Unmarshal(data, &hint)
				pp.Cards = append(pp.Cards, PipelineCard{Op: hint.Op, Definition: string(data)})
			}
		}
		if err == nil {
			switch c.FormValue("action") {
			case "import":
				var imported collections.Pipeline
				imported, err = collections.DecodePipeline([]byte(c.FormValue("definition")))
				if err == nil {
					imported.ID = ""
					imported.Updated = ""
					if pp.Definition.Source != "" {
						imported.Source = pp.Definition.Source
					}
					pp.Definition = imported
					pp.ValidationJSON = ""
					_, _, err = collections.CompilePipeline(imported)
				}
			case "export":
				_, _, err = collections.CompilePipeline(pp.Definition)
				if err == nil {
					b, e := json.MarshalIndent(pp.Definition, "", "  ")
					if e != nil {
						return e
					}
					c.Set("Content-Type", "application/json")
					c.Set("Content-Disposition", `attachment; filename="pipeline.json"`)
					return c.Send(b)
				}
			case "save", "run":
				pp.Definition, err = s.Service.SavePipeline(pp.Definition)
				if err == nil && c.FormValue("action") == "run" {
					_, err = s.Service.PipelineJob(pp.Definition, c.FormValue("format", "json"))
				}
				if err == nil {
					suffix := ""
					if c.FormValue("action") == "run" {
						suffix = "#activity"
					}
					return c.Redirect().Status(303).To("/pipelines?saved=" + pp.Definition.ID + suffix)
				}
			case "search":
			default:
				pp.Through = len(pp.Definition.Steps) - 1
				if n, e := strconv.Atoi(c.FormValue("through")); e == nil {
					pp.Through = n
				}
				if pp.Through < 0 || pp.Through >= len(pp.Definition.Steps) {
					pp.Through = len(pp.Definition.Steps) - 1
				}
				var rows []any
				rows, err = s.Service.PreviewPipeline(c.Context(), pp.Definition, pp.Through)
				if err == nil {
					pp.HasPreview = true
					pp.PreviewStep = pp.Through + 1
					b, e := conversion.Encode(rows, "json")
					if e != nil {
						return e
					}
					pp.Preview = string(b)
					p.Headers, p.Cells = tableRows(rows, collections.Schema(rows))
					p.Count = len(rows)
				}
			}
		}
		if err != nil {
			p.Message = err.Error()
			c.Status(422)
		}
	}
	p.Collections, pp.AvailableCount, err = s.Service.List(pp.SourceQuery, 1, 100)
	if err != nil {
		return err
	}
	if pp.Definition.Source != "" {
		found := false
		for _, col := range p.Collections {
			if col.ID == pp.Definition.Source {
				found = true
				break
			}
		}
		if !found {
			col, e := s.Service.Get(pp.Definition.Source)
			if e == nil {
				p.Collections = append(p.Collections, col)
			}
		}
	}
	if len(pp.Cards) == 0 {
		for _, step := range pp.Definition.Steps {
			b, e := json.MarshalIndent(step, "", "  ")
			if e != nil {
				return e
			}
			pp.Cards = append(pp.Cards, PipelineCard{Op: step.Op, Definition: string(b), ID: fmt.Sprintf("c%d", len(pp.Cards)), Parameters: pipelineParameters(step)})
		}
	}
	if pp.ValidationJSON == "" {
		b, _ := json.MarshalIndent(pp.Definition.Validation, "", "  ")
		pp.ValidationJSON = string(b)
	}
	if pp.ValidationJSON == "null" {
		pp.ValidationJSON = "[]"
	}
	if pp.Definition.ID != "" {
		pp.Schedule, err = s.Service.Schedule(pp.Definition.ID)
		if err != nil {
			return err
		}
		pp.Revisions, err = s.Service.PipelineRevisions(pp.Definition.ID)
		if err != nil {
			return err
		}
		pp.Runs, err = s.Service.PipelineRuns(pp.Definition.ID)
		if err != nil {
			return err
		}
	}
	return c.Render("pages/pipelines", p, "layouts/base")
}

func (s *Server) schedulePipeline(c fiber.Ctx) error {
	interval, err := strconv.Atoi(c.FormValue("interval"))
	if err != nil {
		return fiber.NewError(400, "Choose a valid interval in seconds.")
	}
	err = s.Service.SaveSchedule(collections.Schedule{Pipeline: c.Params("pipeline"), Interval: interval, Format: c.FormValue("format", "ndjson"), Enabled: c.FormValue("enabled") == "on"})
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Redirect().Status(303).To("/pipelines?saved=" + c.Params("pipeline"))
}

func (s *Server) restorePipeline(c fiber.Ctx) error {
	revision, err := strconv.Atoi(c.FormValue("revision"))
	if err != nil {
		return fiber.NewError(400, "Choose a valid revision.")
	}
	p, err := s.Service.RestorePipeline(c.Params("pipeline"), revision)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Redirect().Status(303).To("/pipelines?saved=" + p.ID)
}
func (s *Server) deletePipeline(c fiber.Ctx) error {
	if err := s.Service.DeletePipeline(c.Params("pipeline")); err != nil {
		return err
	}
	return c.Redirect().Status(303).To("/pipelines")
}
func (s *Server) exportPipeline(c fiber.Ctx) error {
	p, err := s.Service.GetPipeline(c.Params("pipeline"))
	if err != nil {
		return fiber.NewError(404, "Pipeline not found.")
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", `attachment; filename="pipeline.json"`)
	return c.Send(b)
}
