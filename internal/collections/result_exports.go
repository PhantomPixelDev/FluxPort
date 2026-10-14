package collections

import (
	"encoding/json"
	"fmt"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
)

func (s *Service) ExportResultJob(owner, resultID, format string, q Query, options conversion.Options) (jobs.Job, error) {
	q = NormalizeInputQuery(q)
	if err := ValidateQuery(q); err != nil {
		return jobs.Job{}, err
	}
	format, options = exportFormat(format, options)
	if !datasetSupported(format) {
		return jobs.Job{}, fmt.Errorf("unsupported output format")
	}
	prep, err := s.Jobs.Get(resultID)
	if err != nil {
		return jobs.Job{}, err
	}
	if prep.Kind != "prepare_result" || prep.Resource != owner {
		return jobs.Job{}, fmt.Errorf("cleaned result does not belong to this collection")
	}
	c, err := s.Get(owner)
	if err != nil {
		return jobs.Job{}, err
	}
	if prep.State != "queued" && prep.State != "running" && prep.State != "ready" {
		return jobs.Job{}, fmt.Errorf("retry cleaned data preparation before exporting: %s", prep.Error)
	}
	p := convertPayload{CollectionID: owner, ResultID: resultID, To: format, Query: &q, Options: options}
	total := c.Size
	if prep.State == "ready" {
		d, e := s.PreparedResult(owner, resultID)
		if e != nil {
			return jobs.Job{}, e
		}
		total = d.Bytes
	} else {
		p.DependsOn = resultID
	}
	b, err := json.Marshal(p)
	if err != nil {
		return jobs.Job{}, err
	}
	name := c.Name + " · cleaned data → " + format
	if p.DependsOn != "" {
		return s.Jobs.SubmitWaiting("convert", owner, name, string(b), total)
	}
	return s.Jobs.Submit("convert", owner, name, string(b), total)
}

func (s *Service) exportFields(c Collection, p convertPayload) ([]Field, error) {
	if p.ResultRef != nil {
		return p.ResultRef.fields, nil
	}
	return s.Fields(c)
}
