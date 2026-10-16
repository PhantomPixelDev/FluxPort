package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxport/internal/jobs"
	"fluxport/internal/resources"
)

type PipelineStepReport struct {
	Step       int    `json:"step"`
	Operation  string `json:"operation"`
	RowsIn     int64  `json:"rows_in"`
	RowsOut    int64  `json:"rows_out"`
	DurationMS int64  `json:"duration_ms"`
}
type PipelineAudit struct {
	Pipeline    Pipeline             `json:"pipeline"`
	Generations map[string]string    `json:"generations"`
	Steps       []PipelineStepReport `json:"steps"`
	Validation  string               `json:"validation"`
}
type pipelineAuditPayload struct {
	Pipeline    Pipeline
	Generations map[string]string
}

func (s *Service) PipelineAuditJob(p Pipeline) (jobs.Job, error) {
	_, ids, err := CompilePipeline(p)
	if err != nil {
		return jobs.Job{}, err
	}
	generations := map[string]string{}
	for _, id := range ids {
		c, e := s.Get(id)
		if e != nil {
			return jobs.Job{}, e
		}
		c, e = s.ready(c)
		if e != nil {
			return jobs.Job{}, e
		}
		generations[id] = c.Generation
	}
	b, err := json.Marshal(pipelineAuditPayload{p, generations})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("pipeline_audit", p.Source, p.Name+" · execution report", string(b), int64(len(p.Steps)+1))
}
func (s *Service) pipelineAuditJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var payload pipelineAuditPayload
	d := json.NewDecoder(strings.NewReader(j.Payload))
	d.UseNumber()
	if err := d.Decode(&payload); err != nil {
		return "", err
	}
	report := PipelineAudit{Pipeline: payload.Pipeline, Generations: payload.Generations, Steps: []PipelineStepReport{}, Validation: "Counts describe complete step outputs. Final validation is enforced by normal pipeline exports."}
	var previous int64
	for through := -1; through < len(payload.Pipeline.Steps); through++ {
		p := payload.Pipeline
		p.Steps = p.Steps[:through+1]
		p.Validation = nil
		text, ids, args, err := compileInputPipeline(p)
		if err != nil {
			return "", err
		}
		var count int64
		start := time.Now()
		_, err = s.sqlRunArgs(ctx, ids, "SELECT count(*) FROM ("+text+")", args, 1, 5*time.Minute, func(_ []string, row []any) error {
			if len(row) != 1 {
				return fmt.Errorf("invalid count result")
			}
			switch n := row[0].(type) {
			case int64:
				count = n
			case json.Number:
				count, err = n.Int64()
			default:
				return fmt.Errorf("unexpected count type %T", n)
			}
			return err
		}, payload.Generations)
		if err != nil {
			return "", fmt.Errorf("step %d: %w", through+1, err)
		}
		if through >= 0 {
			report.Steps = append(report.Steps, PipelineStepReport{through + 1, p.Steps[through].Op, previous, count, time.Since(start).Milliseconds()})
		}
		previous = count
		progress(int64(through+2), count)
		if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
			return "", err
		}
	}
	path := filepath.Join(s.DownloadDir, j.ID+".json")
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer func() { f.Close(); os.Remove(tmp) }()
	err = json.NewEncoder(&resources.Writer{W: f, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck}).Encode(report)
	if err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
