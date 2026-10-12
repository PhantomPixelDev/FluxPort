package web

import (
	"context"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"strings"
	"time"
)

type JoinColumnChoice struct{ Dataset, Name, Label string }
type JoinPreviewSummary struct{ Checked, Matched, Unmatched, Multiple int }

func (s *Server) validateJoinChoice(col collections.Collection, steps []collections.PipelineStep, c fiber.Ctx) error {
	fields, err := s.Service.Fields(col)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, field := range fields {
		known[field.Name] = true
	}
	for _, step := range steps {
		for from, to := range step.Rules.Rename {
			delete(known, from)
			known[to] = true
		}
		for _, name := range step.Rules.Drop {
			delete(known, name)
		}
		if step.Join != nil {
			known[pointerKey(step.Join.As)] = true
		}
		for name := range step.Set {
			known[name] = true
		}
	}
	if !known[c.FormValue("join_left")] {
		return fmt.Errorf("Choose an existing column from this dataset")
	}
	if known[strings.TrimSpace(c.FormValue("join_as"))] {
		return fmt.Errorf("The output column already exists; choose a new column name")
	}
	other, err := s.Service.Get(c.FormValue("join_collection"))
	if err != nil {
		return fmt.Errorf("Choose an existing dataset")
	}
	if other.Status != "ready" {
		return fmt.Errorf("The other dataset is not ready yet")
	}
	fields, err = s.Service.Fields(other)
	if err != nil {
		return err
	}
	for _, field := range fields {
		if field.Name == c.FormValue("join_right") {
			return nil
		}
	}
	return fmt.Errorf("Choose an existing column from the other dataset")
}

func pointerKey(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(path, "/"), "~1", "/"), "~0", "~")
}
func (s *Server) joinPreviewSummary(c fiber.Ctx, p collections.Pipeline) (*JoinPreviewSummary, error) {
	join := p.Steps[len(p.Steps)-1].Join
	prior := p
	prior.Steps = append([]collections.PipelineStep{}, p.Steps[:len(p.Steps)-1]...)
	prior.Validation = nil
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	rows, err := s.Service.PreviewPipeline(ctx, prior, -1)
	if err != nil {
		return nil, err
	}
	keys := map[string]int{}
	leftKeys := []string{}
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Join requires object records")
		}
		value, exists := obj[pointerKey(join.Left)]
		key := ""
		if exists && value != nil {
			b, e := conversion.MarshalJSON(value)
			if e != nil {
				return nil, e
			}
			key = string(b)
			keys[key] = 0
		}
		leftKeys = append(leftKeys, key)
	}
	other, err := s.Service.Get(join.Collection)
	if err != nil {
		return nil, err
	}
	err = s.Service.EachQuery(ctx, other, collections.Query{}, other.Generation, func(row any) error {
		if obj, ok := row.(map[string]any); ok {
			v, exists := obj[pointerKey(join.Right)]
			if exists && v != nil {
				b, e := conversion.MarshalJSON(v)
				if e != nil {
					return e
				}
				key := string(b)
				if _, ok := keys[key]; ok {
					keys[key]++
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := &JoinPreviewSummary{Checked: len(rows)}
	for _, key := range leftKeys {
		if key == "" || keys[key] == 0 {
			result.Unmatched++
		} else {
			result.Matched++
			if keys[key] > 1 {
				result.Multiple++
			}
		}
	}
	return result, nil
}
