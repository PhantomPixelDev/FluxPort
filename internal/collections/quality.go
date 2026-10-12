package collections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"fluxport/internal/quality"
)

type QualityRequest struct {
	Column string         `json:"column,omitempty"`
	Limit  int            `json:"limit,omitempty"`
	Rules  []quality.Rule `json:"rules,omitempty"`
}
type QualityReport struct {
	DuplicateRows    int64              `json:"duplicate_rows"`
	DuplicatesCapped bool               `json:"duplicates_capped"`
	Collection       string             `json:"collection"`
	Generation       string             `json:"generation"`
	Total            int64              `json:"total_records"`
	Checked          int64              `json:"records_checked"`
	Partial          bool               `json:"partial"`
	DurationMS       int64              `json:"duration_ms"`
	SourceBytes      int64              `json:"source_bytes"`
	Columns          []quality.Column   `json:"columns"`
	Validation       quality.Validation `json:"validation"`
}

// Quality reads one immutable index generation, retaining at most bounded column
// statistics and 200 issues. It never loads or changes the complete source.
func (s *Service) Quality(ctx context.Context, c Collection, request QualityRequest) (QualityReport, error) {
	start := time.Now()
	var out QualityReport
	if request.Limit == 0 {
		request.Limit = 10000
	}
	if request.Limit < 1 || request.Limit > 100000 {
		return out, fmt.Errorf("scan limit must be between 1 and 100000")
	}
	validator, err := quality.NewValidator(request.Rules)
	if err != nil {
		return out, err
	}
	c, err = s.ready(c)
	if err != nil {
		return out, err
	}
	out = QualityReport{Collection: c.ID, Generation: c.Generation, Total: c.Rows, SourceBytes: c.Size}
	profiler := quality.NewProfiler()
	if request.Column != "" {
		if err = profiler.EnsureColumn(request.Column); err != nil {
			return out, err
		}
	}
	stop := errors.New("scan complete")
	err = s.EachQuery(ctx, c, Query{}, c.Generation, func(row any) error {
		if out.Checked >= int64(request.Limit) {
			return stop
		}
		if e := profiler.AddColumn(row, request.Column); e != nil {
			return e
		}
		if e := validator.Check(row); e != nil {
			return e
		}
		out.Checked++
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return QualityReport{}, err
	}
	out.Partial = out.Checked < out.Total
	out.Columns = profiler.Columns()
	out.DuplicateRows, out.DuplicatesCapped = profiler.DuplicateRows, profiler.RowsCapped
	out.Validation = validator.Report
	out.DurationMS = time.Since(start).Milliseconds()
	return out, nil
}
func (s *Service) SaveValidationRules(id string, rules []quality.Rule) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	if _, err := quality.NewValidator(rules); err != nil {
		return err
	}
	b, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if len(b) > 65536 {
		return fmt.Errorf("rules exceed 64 KiB")
	}
	_, err = s.DB.Exec(`INSERT INTO validation_rules(collection_id,rules_json) VALUES(?,?) ON CONFLICT(collection_id) DO UPDATE SET rules_json=excluded.rules_json`, id, string(b))
	return err
}
func (s *Service) ValidationRules(id string) ([]quality.Rule, error) {
	var text string
	err := s.DB.QueryRow(`SELECT rules_json FROM validation_rules WHERE collection_id=?`, id).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return []quality.Rule{}, nil
	}
	if err != nil {
		return nil, err
	}
	var rules []quality.Rule
	err = json.Unmarshal([]byte(text), &rules)
	return rules, err
}
