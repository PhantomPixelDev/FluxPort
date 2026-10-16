package web

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/quality"
	"github.com/gofiber/fiber/v3"
)

type QualityPage struct {
	Contract    string
	Comparison  *collections.Comparison
	Differences string
	Other, Keys string
	Report      *collections.QualityReport
	Rules       string
	Limit       int
	Error       string
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
	p.Collections, _, err = s.Service.List("", 1, 100)
	if err != nil {
		return err
	}
	contract, err := s.Service.Contract(col.ID)
	if err != nil {
		return err
	}
	if contract != nil {
		b, _ := json.MarshalIndent(contract, "", "  ")
		q.Contract = string(b)
	}
	rules, err := s.Service.ValidationRules(col.ID)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(rules, "", "  ")
	q.Rules = string(b)
	engineering := c.Method() == "POST" && (c.FormValue("action") == "compare" || c.FormValue("action") == "capture_contract" || c.FormValue("action") == "save_contract" || c.FormValue("action") == "remove_contract" || c.FormValue("action") == "quality_export")
	if engineering {
		switch c.FormValue("action") {
		case "compare":
			q.Other, q.Keys = c.FormValue("other"), c.FormValue("keys")
			var keys []string
			if err = json.Unmarshal([]byte(q.Keys), &keys); err == nil {
				var report collections.Comparison
				report, err = s.Service.Compare(c.Context(), col.ID, collections.CompareRequest{Other: q.Other, Keys: keys})
				if err == nil {
					q.Comparison = &report
					b, _ := json.MarshalIndent(report.Sample, "", "  ")
					q.Differences = string(b)
				}
			}
		case "capture_contract":
			contract, err = s.Service.CaptureContract(col.ID)
			if err == nil {
				err = s.Service.SaveContract(col.ID, contract)
			}
			if err == nil {
				b, _ := json.MarshalIndent(contract, "", "  ")
				q.Contract = string(b)
				p.Message = "Contract captured. Review expected types below."
			}
		case "save_contract":
			q.Contract = c.FormValue("contract")
			if len(q.Contract) > 65536 {
				err = fiber.NewError(422, "Contract exceeds 64 KiB.")
			} else {
				var value quality.Contract
				d := json.NewDecoder(bytes.NewBufferString(q.Contract))
				d.DisallowUnknownFields()
				err = d.Decode(&value)
				if err == nil {
					err = s.Service.SaveContract(col.ID, &value)
					contract = &value
				}
			}
			if err == nil {
				p.Message = "Contract saved."
			}
		case "remove_contract":
			err = s.Service.SaveContract(col.ID, nil)
			if err == nil {
				contract = nil
				q.Contract = ""
				p.Message = "Contract removed."
			}
		case "quality_export":
			var jobID string
			job, e := s.Service.QualityExportJob(col.ID, c.FormValue("validation_mode"))
			err = e
			jobID = job.ID
			if err == nil {
				return c.Redirect().Status(303).To("/collections/" + col.ID + "/quality?quality_job=" + jobID)
			}
		}
		if err != nil {
			q.Error = err.Error()
			c.Status(422)
		}
	}
	if jobID := c.Query("quality_job"); jobID != "" {
		j, e := s.Service.Jobs.Get(jobID)
		if e != nil || j.Resource != col.ID || j.Kind != "quality_export" {
			return fiber.NewError(404, "Validation export not found.")
		}
		p.FileJob = &j
		p.Jobs = []jobs.Job{j}
		p.JobsResource = col.ID
	}
	if c.Method() == "POST" && !engineering {
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
		report, e := s.Service.Quality(ctx, col, collections.QualityRequest{Limit: q.Limit, Rules: rules, Contract: contract})
		if e != nil {
			q.Error = e.Error()
		} else {
			q.Report = &report
		}
	}
	return s.renderCollectionView(c, "quality", p)
}
