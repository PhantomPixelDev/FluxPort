package web

import (
	"bytes"
	"io"
	"mime/multipart"

	"strings"
	"testing"

	"fluxport/internal/collections"
	"fluxport/internal/transformations"
)

// Task 6 of docs/superpowers/plans/2026-10-17-ui-ux-simplification.md: state
// words are plain ("Uploaded", not raw "staged"), and the schedule block
// speaks "unix time" rather than "Unix seconds". Save-status copy lives in
// JS and is verified by grep in the same task.
func TestStateLanguageReadsPlainly(t *testing.T) {
	app, service, col := setup(t, "", "")
	visit, body := call(t, app, "GET", "/collections", "", nil, nil)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("csrf", csrf(t, body))
	part, _ := writer.CreateFormFile("file", "later.csv")
	io.WriteString(part, "name\nAda\n")
	writer.Close()
	imported, upHTML := call(t, app, "POST", "/collections/import", buf.String(), map[string]string{"Content-Type": writer.FormDataContentType()}, visit.Cookies())
	if imported.StatusCode != 200 && imported.StatusCode != 303 {
		t.Fatal(imported.StatusCode, upHTML)
	}
	_, jobsBody := call(t, app, "GET", "/jobs", "", map[string]string{"HX-Request": "true"}, visit.Cookies())
	for _, raw := range []string{">ready<", ">staged<", ">queued<", ">running<", ">failed<", ">cancelled<", ">interrupted<"} {
		if strings.Contains(jobsBody, raw) {
			t.Fatalf("raw job state %q leaked to the jobs list: %s", strings.Trim(raw, "<>"), snippet(jobsBody, "state-label"))
		}
	}
	if !strings.Contains(jobsBody, "Done") {
		t.Fatal("finished jobs do not read Done")
	}
	if _, err := service.SavePipeline(collections.Pipeline{Name: "Trim names", Source: col.ID, Steps: []collections.PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}); err != nil {
		t.Fatal(err)
	}
	page, pipelinePage := call(t, app, "GET", "/pipelines?saved="+pipelineID(t, service), "", nil, visit.Cookies())
	if page.StatusCode != 200 {
		t.Fatal(page.StatusCode)
	}
	if !strings.Contains(pipelinePage, "Next run (unix time)") || strings.Contains(pipelinePage, "Unix seconds") || strings.Contains(pipelinePage, "saved recipe") {
	if !strings.Contains(pipelinePage, "Next run (unix time)") || strings.Contains(pipelinePage, "Unix seconds") || strings.Contains(pipelinePage, "saved recipe") {
		t.Fatal("schedule block still uses recipe/Unix-seconds wording")
	}
	}
	if imported.Cookies() == nil {
		t.Fatal("expected import cookies")
	}
}

func snippet(body, marker string) string {
	at := strings.Index(body, marker)
	if at < 0 {
		return "(not found)"
	}
	if len(body) > at+80 {
		return body[at : at+80]
	}
	return body[at:]
}

func pipelineID(t *testing.T, service *collections.Service) string {
	t.Helper()
	saved, err := service.Pipelines()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 {
		t.Fatal("pipeline was not saved")
	}
	return saved[0].ID
}

