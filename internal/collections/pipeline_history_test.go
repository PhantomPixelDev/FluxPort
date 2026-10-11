package collections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxport/internal/quality"
)

func TestPipelineRevisionsRestoreConflictAndRunHistory(t *testing.T) {
	s := testService(t)
	c, err := s.Import("records.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.SavePipeline(Pipeline{Name: "Versioned", Source: c.ID})
	if err != nil || p.Revision != 1 {
		t.Fatal(p, err)
	}
	stale := p
	p.Steps = []PipelineStep{{Op: "limit", Limit: 1}}
	p, err = s.SavePipeline(p)
	if err != nil || p.Revision != 2 {
		t.Fatal(p, err)
	}
	if _, err = s.SavePipeline(stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale save overwrote recipe", err)
	}
	p, err = s.RestorePipeline(p.ID, 1)
	if err != nil || p.Revision != 3 || len(p.Steps) != 0 {
		t.Fatal(p, err)
	}
	history, err := s.PipelineRevisions(p.ID)
	if err != nil || len(history) != 3 {
		t.Fatal(history, err)
	}
	job, err := s.PipelineJob(p, "json")
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.PipelineRuns(p.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != job.ID {
		t.Fatal(runs, err)
	}
}
func TestValidationRejectsPartialPipelineDownload(t *testing.T) {
	s := testService(t)
	c, err := s.Import("invalid.json", strings.NewReader(`[{"id":1},{"name":"missing ID"}]`))
	if err != nil {
		t.Fatal(err)
	}
	p := Pipeline{Name: "Validated", Source: c.ID, Validation: []quality.Rule{{Field: "id", Required: true}}}
	job, err := s.PipelineJob(p, "json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.convertJob(context.Background(), job, func(int64, int64) {}); err == nil {
		t.Fatal("invalid data published")
	}
	if _, err = os.Stat(filepath.Join(s.DownloadDir, job.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("partial download published", err)
	}
	files, _ := os.ReadDir(s.DownloadDir)
	if len(files) != 0 {
		t.Fatal("temporary partial export leaked", files)
	}
}
