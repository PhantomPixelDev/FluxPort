package collections

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/quality"
)

func DecodePipeline(data []byte) (Pipeline, error) {
	var p Pipeline
	if len(data) > 65536 {
		return p, fmt.Errorf("pipeline definition exceeds 64 KiB")
	}
	if _, err := conversion.Decode(data, "json"); err != nil {
		return p, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&p)
	return p, err
}
func (s *Service) SavePipeline(p Pipeline) (Pipeline, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 100 {
		return p, fmt.Errorf("pipeline name must contain 1 to 100 characters")
	}
	if p.InputQuery != nil {
		q := NormalizeInputQuery(*p.InputQuery)
		p.InputQuery = &q
	}
	p.Version = 1
	_, ids, err := CompilePipeline(p)
	if err != nil {
		return p, err
	}
	for _, id := range ids {
		if _, err = s.Get(id); err != nil {
			return p, err
		}
	}
	p.ID = recordID("pipeline", p.Name, 0)
	tx, err := s.DB.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var revision int
	err = tx.QueryRow(`SELECT COALESCE(MAX(revision),0) FROM pipeline_revisions WHERE pipeline_id=?`, p.ID).Scan(&revision)
	if err != nil {
		return p, err
	}
	if revision > 0 && p.Revision > 0 && p.Revision != revision {
		return p, ErrConflict
	}
	p.Revision = revision + 1
	p.Updated = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	if len(data) > 65536 {
		return p, fmt.Errorf("pipeline definition exceeds 64 KiB")
	}
	_, err = tx.Exec(`INSERT INTO pipelines(id,name,definition_json,updated) VALUES(?,?,?,?) ON CONFLICT(name) DO UPDATE SET definition_json=excluded.definition_json,updated=excluded.updated`, p.ID, p.Name, string(data), p.Updated)
	if err != nil {
		return p, err
	}
	if _, err = tx.Exec(`INSERT INTO pipeline_revisions(pipeline_id,revision,definition_json,created) VALUES(?,?,?,?)`, p.ID, p.Revision, string(data), p.Updated); err != nil {
		return p, err
	}
	if _, err = tx.Exec(`DELETE FROM pipeline_revisions WHERE pipeline_id=? AND revision<=?`, p.ID, p.Revision-50); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (s *Service) Pipelines() ([]Pipeline, error) {
	rows, err := s.DB.Query(`SELECT definition_json FROM pipelines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pipeline{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		p, e := DecodePipeline([]byte(data))
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) GetPipeline(id string) (Pipeline, error) {
	var data string
	err := s.DB.QueryRow(`SELECT definition_json FROM pipelines WHERE id=?`, id).Scan(&data)
	if err != nil {
		return Pipeline{}, err
	}
	return DecodePipeline([]byte(data))
}
func (s *Service) DeletePipeline(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM pipeline_schedules WHERE pipeline_id=?`, `DELETE FROM pipeline_revisions WHERE pipeline_id=?`, `DELETE FROM pipelines WHERE id=?`} {
		if _, err = tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) PreviewPipeline(ctx context.Context, p Pipeline, through int) ([]any, error) {
	final := through < 0 || through >= len(p.Steps)-1
	if through >= 0 && through < len(p.Steps) {
		p.Steps = p.Steps[:through+1]
	}
	text, ids, args, err := compileInputPipeline(p)
	if err != nil {
		return nil, err
	}
	rows := []any{}
	validator, err := quality.NewValidator(p.Validation)
	if err != nil {
		return nil, err
	}
	bytes := 0
	_, err = s.sqlRunArgs(ctx, ids, text+` LIMIT 20`, args, 0, 10*time.Second, func(_ []string, row []any) error {
		payload, ok := row[0].(string)
		if !ok {
			return fmt.Errorf("pipeline returned invalid record")
		}
		bytes += len(payload)
		if int64(len(payload)) > s.RecordBytes || bytes > 8<<20 {
			return fmt.Errorf("pipeline preview exceeds record or result size limit")
		}
		value, e := conversion.Decode([]byte(payload), "json")
		if e != nil {
			return e
		}
		if final {
			if e = validator.Check(value); e != nil {
				return e
			}
			if validator.Report.Errors > 0 {
				return fmt.Errorf("validation failed at output row %d, column %s: %s", validator.Report.Records, validator.Report.Issues[0].Field, validator.Report.Issues[0].Check)
			}
		}
		rows = append(rows, value)
		return nil
	}, nil)
	return rows, err
}
func (s *Service) PipelineJob(p Pipeline, format string) (jobs.Job, error) {
	if p.InputContract == nil {
		var err error
		p.InputContract, err = s.Contract(p.Source)
		if err != nil {
			return jobs.Job{}, err
		}
	}
	_, ids, err := CompilePipeline(p)
	if err != nil {
		return jobs.Job{}, err
	}
	format, options := exportFormat(format, conversion.Options{SpreadsheetSafe: true})
	if !datasetSupported(format) {
		return jobs.Job{}, fmt.Errorf("unsupported output format")
	}
	generations := map[string]string{}
	var source Collection
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
		if id == p.Source {
			source = c
		}
	}
	data, err := json.Marshal(convertPayload{CollectionID: p.Source, To: format, Pipeline: &p, Generations: generations, Options: options})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("convert", p.Source, p.Name+" · pipeline → "+format, string(data), source.Size)
}

func (s *Service) eachPipeline(ctx context.Context, p Pipeline, generations map[string]string, emit func(any) error) error {
	if err := s.checkPipelineContract(ctx, p, generations); err != nil {
		return err
	}
	if p.Parameters != nil {
		bound, err := BindPipeline(p, nil)
		if err != nil {
			return err
		}
		p = bound
	}
	validator, err := quality.NewValidator(p.Validation)
	if err != nil {
		return err
	}
	text, ids, args, err := compileInputPipeline(p)
	if err != nil {
		return err
	}
	for _, id := range ids {
		c, e := s.Get(id)
		if e != nil {
			return e
		}
		if c.Generation != generations[id] || c.Status != "ready" {
			return ErrConflict
		}
	}
	_, err = s.sqlRunArgs(ctx, ids, text, args, 0, 30*time.Minute, func(_ []string, row []any) error {
		payload, ok := row[0].(string)
		if !ok {
			return fmt.Errorf("pipeline returned invalid record")
		}
		if int64(len(payload)) > s.RecordBytes {
			return fmt.Errorf("pipeline record exceeds %d-byte limit", s.RecordBytes)
		}
		value, e := conversion.Decode([]byte(payload), "json")
		if e != nil {
			return e
		}
		if e = validator.Check(value); e != nil {
			return e
		}
		if validator.Report.Errors > 0 {
			return fmt.Errorf("validation rejected output row %d, column %s: %s; no output published", validator.Report.Records, validator.Report.Issues[0].Field, validator.Report.Issues[0].Check)
		}
		return emit(value)
	}, generations)
	return err
}

type PipelineRevision struct {
	Revision int    `json:"revision"`
	Created  string `json:"created"`
}

func (s *Service) PipelineRevisions(id string) ([]PipelineRevision, error) {
	rows, err := s.DB.Query(`SELECT revision,created FROM pipeline_revisions WHERE pipeline_id=? ORDER BY revision DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PipelineRevision{}
	for rows.Next() {
		var r PipelineRevision
		if err = rows.Scan(&r.Revision, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) RestorePipeline(id string, revision int) (Pipeline, error) {
	var text string
	if err := s.DB.QueryRow(`SELECT definition_json FROM pipeline_revisions WHERE pipeline_id=? AND revision=?`, id, revision).Scan(&text); err != nil {
		return Pipeline{}, err
	}
	p, err := DecodePipeline([]byte(text))
	if err != nil {
		return p, err
	}
	p.Revision = 0
	return s.SavePipeline(p)
}
func (s *Service) PipelineRuns(id string) ([]jobs.Job, error) {
	if _, err := s.GetPipeline(id); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`SELECT id FROM jobs WHERE kind IN ('convert','pipeline_audit') AND json_extract(payload,'$.Pipeline.id')=? ORDER BY created DESC LIMIT 50`, id)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []jobs.Job{}
	for _, id := range ids {
		j, e := s.Jobs.Get(id)
		if e != nil && e != sql.ErrNoRows {
			return nil, e
		}
		if e == nil {
			out = append(out, j)
		}
	}
	return out, nil
}
