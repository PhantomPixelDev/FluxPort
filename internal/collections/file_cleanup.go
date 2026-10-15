package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/quality"
)

type fileCleanupPayload struct {
	Pipeline    Pipeline
	Generations map[string]string
	Label       string
}

func (s *Service) FileCleanupJob(p Pipeline, generation, label string) (jobs.Job, error) {
	if generation == "" {
		return jobs.Job{}, fmt.Errorf("file generation is required")
	}
	if p.InputQuery != nil {
		q := NormalizeInputQuery(*p.InputQuery)
		p.InputQuery = &q
	}
	_, ids, err := CompilePipeline(p)
	if err != nil {
		return jobs.Job{}, err
	}
	source, err := s.Get(p.Source)
	if err != nil {
		return jobs.Job{}, err
	}
	source, err = s.ready(source)
	if err != nil {
		return jobs.Job{}, err
	}
	if source.Generation != generation {
		return jobs.Job{}, ErrConflict
	}
	if source.ReadOnly || source.Size > s.MaxBytes || source.Rows > 100000 {
		return jobs.Job{}, fmt.Errorf("this file is read-only; prepare changes for download")
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
	if len(label) == 0 {
		label = "Applied cleanup"
	}
	if len(label) > 256 {
		label = label[:256]
	}
	data, err := json.Marshal(fileCleanupPayload{Pipeline: p, Generations: generations, Label: label})
	if err != nil {
		return jobs.Job{}, err
	}
	return s.Jobs.Submit("file_cleanup", p.Source, label, string(data), source.Size)
}
func recordsForFileQuery(ctx context.Context, ref indexReference, q Query) ([]Record, error) {
	q = NormalizeInputQuery(q)
	where, order, args, sortArgs, err := querySQL(q)
	if err != nil {
		return nil, err
	}
	db, err := openReference(ref)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT record_id,ordinal,payload FROM records WHERE "+where+" ORDER BY "+order, append(args, sortArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		if len(out) >= 100000 {
			return nil, fmt.Errorf("file editing supports at most 100000 rows")
		}
		var row Record
		var payload string
		if err = rows.Scan(&row.ID, &row.Ordinal, &payload); err != nil {
			return nil, err
		}
		row.Value, err = conversion.Decode([]byte(payload), "json")
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func (s *Service) fileCleanupJob(ctx context.Context, j jobs.Job, progress func(int64, int64)) (string, error) {
	var request fileCleanupPayload
	decoder := json.NewDecoder(strings.NewReader(j.Payload))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return "", err
	}
	p := request.Pipeline
	if p.Source != j.Resource || request.Generations[p.Source] == "" {
		return "", fmt.Errorf("invalid file cleanup job")
	}
	c, err := s.Get(j.Resource)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	c, err = s.ready(c)
	if err != nil {
		return "", err
	}
	if c.Generation != request.Generations[c.ID] {
		return "", ErrConflict
	}
	if c.ReadOnly || c.Size > s.MaxBytes || c.Rows > 100000 {
		return "", fmt.Errorf("this file is read-only")
	}
	before, err := s.read(c.Path)
	if err != nil {
		return "", err
	}
	if fileDigest(before) != c.SourceSHA {
		return "", ErrConflict
	}
	original, err := conversion.Decode(before, c.Format)
	if err != nil {
		return "", err
	}
	sourceRows := conversion.Rows(original)
	ref, err := s.sourceReference(c, c.Generation)
	if err != nil {
		return "", err
	}
	defer ref.release()
	all, err := recordsForFileQuery(ctx, ref, Query{})
	if err != nil {
		return "", err
	}
	if len(all) != len(sourceRows) {
		return "", fmt.Errorf("source record structure does not support file editing")
	}
	input := Query{}
	if p.InputQuery != nil {
		input = NormalizeInputQuery(*p.InputQuery)
	}
	selected, err := recordsForFileQuery(ctx, ref, input)
	if err != nil {
		return "", err
	}
	// Rank each selected source row. Position-preserving steps can be merged back
	// into its exact source location, even if the captured view was sorted.
	p.InputQuery = &input
	text, ids, args, err := compileInputPipeline(p)
	if err != nil {
		return "", err
	}
	suffix := fmt.Sprintf(" SELECT payload FROM s%d ORDER BY position", len(p.Steps))
	replacement := fmt.Sprintf(" SELECT position,payload FROM s%d ORDER BY position", len(p.Steps))
	if !strings.HasSuffix(text, suffix) {
		return "", fmt.Errorf("invalid cleanup statement")
	}
	text = strings.TrimSuffix(text, suffix) + replacement
	validator, err := quality.NewValidator(p.Validation)
	if err != nil {
		return "", err
	}
	var result []any
	var positions []int64
	var outputBytes int64
	_, err = s.sqlRunArgs(ctx, ids, text, args, 0, 90*time.Second, func(_ []string, row []any) error {
		if len(result) >= 100000 {
			return fmt.Errorf("cleanup exceeds 100000 output records")
		}
		position, ok := row[0].(int64)
		if !ok {
			return fmt.Errorf("cleanup returned an invalid source position")
		}
		payload, ok := row[1].(string)
		if !ok {
			return fmt.Errorf("cleanup returned an invalid record")
		}
		outputBytes += int64(len(payload))
		if int64(len(payload)) > s.RecordBytes || outputBytes > s.MaxBytes {
			return ErrTooLarge
		}
		value, e := conversion.Decode([]byte(payload), "json")
		if e != nil {
			return e
		}
		if e = validator.Check(value); e != nil {
			return e
		}
		if validator.Report.Errors > 0 {
			return fmt.Errorf("validation rejected a cleanup record; file left unchanged")
		}
		result = append(result, value)
		positions = append(positions, position)
		processed := int64(len(result)) * c.Size / max(int64(1), c.Rows)
		progress(min(processed, c.Size), int64(len(result)))
		return nil
	}, request.Generations)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	preservePosition := true
	for _, step := range p.Steps {
		switch step.Op {
		case "sort", "join", "append", "group", "aggregate":
			preservePosition = false
		}
	}
	matched := map[int64]bool{}
	for _, row := range selected {
		matched[row.Ordinal] = true
	}
	afterIDs := []string{}
	afterRows := []any{}
	if preservePosition {
		updates := map[int64]any{}
		for i, position := range positions {
			if position < 1 || position > int64(len(selected)) {
				return "", fmt.Errorf("cleanup source position is outside captured input")
			}
			ordinal := selected[position-1].Ordinal
			if _, exists := updates[ordinal]; exists {
				return "", fmt.Errorf("cleanup duplicated a source position")
			}
			updates[ordinal] = result[i]
		}
		for i, row := range sourceRows {
			if !matched[int64(i)] {
				afterRows = append(afterRows, row)
				afterIDs = append(afterIDs, all[i].ID)
				continue
			}
			if value, exists := updates[int64(i)]; exists {
				afterRows = append(afterRows, value)
				afterIDs = append(afterIDs, all[i].ID)
			}
		}
	} else {
		if len(selected) == 0 && len(result) > 0 {
			return "", fmt.Errorf("no source rows match; choose the entire file before adding derived rows")
		}
		next, last := 0, int64(-1)
		for _, row := range selected {
			last = max(last, row.Ordinal)
		}
		for i, row := range sourceRows {
			if !matched[int64(i)] {
				afterRows = append(afterRows, row)
				afterIDs = append(afterIDs, all[i].ID)
				continue
			}
			if next < len(result) {
				afterRows = append(afterRows, result[next])
				afterIDs = append(afterIDs, newRecordID())
				next++
			}
			if int64(i) == last {
				for next < len(result) {
					afterRows = append(afterRows, result[next])
					afterIDs = append(afterIDs, newRecordID())
					next++
				}
			}
		}
	}
	after, err := encodeFileRows(c, original, afterRows)
	if err != nil {
		return "", err
	}
	if err = s.validateFileOutput(c, after, len(afterRows)); err != nil {
		return "", err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	c, err = s.ready(c, true)
	if err != nil {
		return "", err
	}
	if c.Generation != request.Generations[c.ID] {
		return "", ErrConflict
	}
	for id, generation := range request.Generations {
		dependency, e := s.Get(id)
		if e != nil {
			return "", e
		}
		dependency, e = s.ready(dependency, true)
		if e != nil {
			return "", e
		}
		if dependency.Generation != generation {
			return "", ErrConflict
		}
	}
	beforeIDs := make([]string, len(all))
	for i, row := range all {
		beforeIDs[i] = row.ID
	}
	if err = s.publishFileRevision(ctx, c, before, after, beforeIDs, afterIDs, request.Label, j.ID); err != nil {
		return "", err
	}
	progress(c.Size, int64(len(afterRows)))
	return "file-saved", nil
}
