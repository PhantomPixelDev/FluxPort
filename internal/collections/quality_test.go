package collections

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"fluxport/internal/quality"
)

func TestQualityGenerationRulesAndCancellation(t *testing.T) {
	s := testService(t)
	c, err := s.Import("quality.json", strings.NewReader(`[{"id":9007199254740993,"name":"Ada"},{"id":9007199254740992,"name":null},{"id":3}]`))
	if err != nil {
		t.Fatal(err)
	}
	min := json.Number("9007199254740993")
	rules := []quality.Rule{{Field: "id", Type: "integer", Min: &min}, {Field: "name", Required: true, NonNull: true}}
	if err = s.SaveValidationRules(c.ID, rules); err != nil {
		t.Fatal(err)
	}
	saved, err := s.ValidationRules(c.ID)
	if err != nil || len(saved) != 2 {
		t.Fatal(saved, err)
	}
	report, err := s.Quality(context.Background(), c, QualityRequest{Limit: 2, Rules: saved})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Partial || report.Checked != 2 || report.Total != 3 || report.Generation == "" || report.Validation.Errors != 2 {
		t.Fatal(report)
	}
	report, err = s.Quality(context.Background(), c, QualityRequest{Rules: saved})
	if err != nil || report.Partial || report.Validation.Invalid != 2 {
		t.Fatal(report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.Quality(ctx, c, QualityRequest{}); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err = s.Quality(context.Background(), c, QualityRequest{Limit: 100001}); err == nil {
		t.Fatal("limit ignored")
	}
}
