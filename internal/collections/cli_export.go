package collections

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
)

// ExportFile executes the same bounded export handler used by background jobs.
// It returns a managed download path; callers own its cleanup after copying.
func (s *Service) ExportFile(ctx context.Context, id, format string, pipeline *Pipeline) (string, error) {
	format = conversion.Format(format)
	if !datasetSupported(format) {
		return "", fmt.Errorf("unsupported export format")
	}
	c, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if _, err = s.ready(c); err != nil {
		return "", err
	}
	p := convertPayload{CollectionID: id, To: format, Pipeline: pipeline}
	if pipeline != nil {
		_, ids, e := CompilePipeline(*pipeline)
		if e != nil {
			return "", e
		}
		p.Generations = map[string]string{}
		for _, source := range ids {
			col, e := s.Get(source)
			if e != nil {
				return "", e
			}
			p.Generations[source] = col.Generation
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	path, err := s.convertJob(ctx, jobs.Job{ID: jobs.ID(), Kind: "convert", Resource: id, Total: c.Size, Payload: string(b)}, func(int64, int64) {})
	if err != nil && path != "" {
		os.Remove(path)
	}
	return path, err
}
