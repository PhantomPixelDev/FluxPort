package web

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/transformations"
	"github.com/gofiber/fiber/v3"
)

func (s *Server) datasetChanges(c fiber.Ctx) error { return s.pipelines(c) }

func cleanupStepJSON(step collections.PipelineStep) string {
	b, _ := json.Marshal(step)
	return string(b)
}

func defaultCleanupName(name string) string {
	result := "Cleaned " + name
	for len(result) > 100 {
		chars := []rune(result)
		result = string(chars[:len(chars)-1])
	}
	return result
}

// These controls produce the same steps used by the advanced editor and CLI.
func cleanupStep(c fiber.Ctx) (collections.PipelineStep, error) {
	kind, field := c.FormValue("cleanup"), strings.TrimSpace(c.FormValue("field"))
	step := collections.PipelineStep{}
	switch kind {
	case "deduplicate":
		step.Op = "deduplicate"
		if field != "" {
			step.Keys = []string{field}
		}
	case "rename":
		to := strings.TrimSpace(c.FormValue("new_name"))
		if field == "" || to == "" || len(field) > 256 || len(to) > 256 {
			return step, fmt.Errorf("Choose a column and enter its new name")
		}
		step.Op = "rename"
		step.Rules = transformations.Rules{Rename: map[string]string{field: to}}
	case "drop":
		if field == "" {
			return step, fmt.Errorf("Choose a column to remove")
		}
		step.Op = "drop"
		step.Rules = transformations.Rules{Drop: []string{field}}
	case "sort":
		if field == "" {
			return step, fmt.Errorf("Choose a column to sort")
		}
		step.Op = "sort"
		step.Sort = []collections.Sort{{Field: field, Desc: c.FormValue("direction") == "desc"}}
	case "trim", "lower", "upper", "default":
		step.Op = "transform"
		op := &transformations.Operation{Kind: kind, Field: field}
		if kind == "default" {
			op.Value = c.FormValue("fill_value")
		}
		if err := op.Validate(); err != nil {
			return step, err
		}
		step.Transform = op
	default:
		return step, fmt.Errorf("Choose a cleanup action")
	}
	return step, nil
}

func cleanupStepName(step collections.PipelineStep) string {
	switch step.Op {
	case "deduplicate":
		if len(step.Keys) > 0 {
			return "Remove duplicates by " + strings.Join(step.Keys, ", ")
		}
		return "Remove identical rows"
	case "rename":
		key := firstMapKey(step.Rules.Rename)
		return "Rename " + key + " to " + step.Rules.Rename[key]
	case "drop":
		return "Remove columns: " + strings.Join(step.Rules.Drop, ", ")
	case "sort":
		if len(step.Sort) > 0 {
			direction := "ascending"
			if step.Sort[0].Desc {
				direction = "descending"
			}
			return "Sort " + step.Sort[0].Field + " " + direction
		}
	case "transform":
		if step.Transform != nil {
			label := map[string]string{"trim": "Trim spaces in ", "lower": "Lowercase ", "upper": "Uppercase ", "default": "Fill empty values in "}[step.Transform.Kind]
			if label != "" {
				return label + step.Transform.Field
			}
		}
	}
	return "Advanced step: " + step.Op
}

func adjustCleanup(c fiber.Ctx, steps []collections.PipelineStep) ([]collections.PipelineStep, error) {
	switch action := c.FormValue("action"); {
	case action == "add":
		if len(steps) >= 20 {
			return steps, fmt.Errorf("A set of changes supports up to 20 steps")
		}
		step, err := cleanupStep(c)
		if err != nil {
			return steps, err
		}
		return append(steps, step), nil
	case action == "undo":
		if len(steps) > 0 {
			return steps[:len(steps)-1], nil
		}
	case action == "reset":
		return []collections.PipelineStep{}, nil
	case strings.HasPrefix(action, "remove:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "remove:"))
		if err != nil || i < 0 || i >= len(steps) {
			return steps, fmt.Errorf("Step not found")
		}
		return append(steps[:i], steps[i+1:]...), nil
	}
	return steps, nil
}

func (s *Server) enableDatasetEditing(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	if col.ReadOnly {
		return s.workbenchFailure(c, fmt.Errorf("This dataset is too large for source editing. Use Changes to prepare a cleaned download"))
	}
	if err = s.Service.Update(col.ID, col.Enabled, true, col.Rules); err != nil {
		return s.workbenchFailure(c, err)
	}
	return c.Redirect().Status(303).To("/collections/" + col.ID)
}
