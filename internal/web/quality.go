package web

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

type QualityPage struct {
	Report *collections.QualityReport
	Rules  string
	Limit  int
	Error  string
}

func (s *Server) qualityPage(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	p := s.base(c, "Data quality", "collections")
	p.Collection = col
	q := &QualityPage{Limit: 10000}
	p.Quality = q
	rules, err := s.Service.ValidationRules(col.ID)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rules, "", "  ")
	q.Rules = string(b)
	if c.Method() == "POST" {
		q.Rules = c.FormValue("rules")
		q.Limit, err = strconv.Atoi(c.FormValue("limit"))
		if err != nil {
			q.Error = "Choose a valid record limit."
		}
		if len(q.Rules) > 65536 {
			q.Error = "Rules exceed 64 KiB."
		} else if _, e := conversion.Decode([]byte(q.Rules), "json"); e != nil {
			q.Error = e.Error()
		} else {
			d := json.NewDecoder(bytes.NewBufferString(q.Rules))
			d.UseNumber()
			d.DisallowUnknownFields()
			if e = d.Decode(&rules); e != nil {
				q.Error = e.Error()
			}
		}
		if q.Error == "" && c.FormValue("action") == "save" {
			if e := s.Service.SaveValidationRules(col.ID, rules); e != nil {
				q.Error = e.Error()
			} else {
				p.Message = "Validation rules saved."
			}
		}
	}
	if q.Error == "" {
		ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
		defer cancel()
		report, e := s.Service.Quality(ctx, col, collections.QualityRequest{Limit: q.Limit, Rules: rules})
		if e != nil {
			q.Error = e.Error()
		} else {
			q.Report = &report
		}
	}
	return s.renderCollectionView(c, "quality", p)
}
