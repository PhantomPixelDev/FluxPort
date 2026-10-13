package web

import (
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"github.com/gofiber/fiber/v3"
)

func downloadJobFormat(job jobs.Job) string {
	var payload struct {
		To      string
		Options conversion.Options
	}
	if json.Unmarshal([]byte(job.Payload), &payload) != nil {
		return ""
	}
	if payload.To == "parquet" && payload.Options.ParquetTyped {
		return "parquet-typed"
	}
	for _, format := range collections.DatasetFormats() {
		if format == payload.To {
			return format
		}
	}
	return ""
}

func downloadJobSteps(job jobs.Job) string {
	var payload struct{ Pipeline *struct{ ID string } }
	if json.Unmarshal([]byte(job.Payload), &payload) == nil && payload.Pipeline != nil {
		return payload.Pipeline.ID
	}
	return ""
}

func downloadJobDraft(job jobs.Job) string {
	var payload struct{ Pipeline *collections.Pipeline }
	if json.Unmarshal([]byte(job.Payload), &payload) == nil && payload.Pipeline != nil && payload.Pipeline.ID == "" {
		data, _ := json.Marshal(payload.Pipeline)
		return string(data)
	}
	return ""
}

// Current cleanup can be downloaded without creating or overwriting saved steps.
func (s *Server) downloadPipeline(c fiber.Ctx, col collections.Collection) (*collections.Pipeline, error) {
	dataset := c.FormValue("dataset", c.Query("dataset"))
	if dataset == "current" {
		p, err := collections.DecodePipeline([]byte(c.FormValue("draft")))
		if err != nil {
			return nil, fmt.Errorf("Unable to read current changes: %w", err)
		}
		p.ID, p.Source, p.Name, p.Version = "", col.ID, "Cleaned data", 1
		p.Revision, p.Updated = 0, ""
		if len(p.Steps) == 0 {
			return nil, fmt.Errorf("Add a cleanup step before downloading cleaned data")
		}
		if len(p.Steps) > 20 {
			return nil, fmt.Errorf("Current cleanup supports at most 20 steps")
		}
		if _, _, err := collections.CompilePipeline(p); err != nil {
			return nil, err
		}
		return &p, nil
	}
	id := c.FormValue("pipeline", c.Query("pipeline"))
	if dataset == "original" {
		return nil, nil
	}
	if dataset != "" {
		id = dataset
	}
	if id == "" {
		return nil, nil
	}
	p, err := s.Service.GetPipeline(id)
	if err != nil || p.Source != col.ID {
		return nil, fmt.Errorf("Saved steps not found for this dataset")
	}
	return &p, nil
}

func downloadFormatName(f string) string {
	switch f {
	case "original":
		return "Original file"
	case "xlsx":
		return "Excel (.xlsx)"
	case "parquet":
		return "Parquet · lossless records (.parquet)"
	case "parquet-typed":
		return "Parquet · typed columns (.parquet)"
	default:
		return strings.ToUpper(f) + " (." + f + ")"
	}
}

func downloadFormatNote(f string) string {
	switch f {
	case "xlsx":
		return "Text cells. Null, missing and empty text become blank."
	case "parquet":
		return "Records are stored in one JSON column."
	case "parquet-typed":
		return "Typed columns. Nested/mixed values become text; missing and null merge."
	case "csv":
		return "Nested values are saved as JSON text."
	case "toml":
		return "Records use a records wrapper. Null values are not supported."
	default:
		return ""
	}
}

func (s *Server) originalDownload(c fiber.Ctx, col collections.Collection) error {
	r, err := s.Service.RawSource(col)
	if err != nil {
		return err
	}
	file := r.(*os.File)
	info, err := file.Stat()
	if err != nil {
		r.Close()
		return err
	}
	name := path.Base(strings.ReplaceAll(col.Name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		name = "collection"
	}
	if !strings.HasSuffix(strings.ToLower(name), "."+col.Format) {
		name += "." + col.Format
	}
	c.Set("Content-Type", conversion.MIME(col.Format))
	c.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	return c.SendStream(r, int(info.Size()))
}

func (s *Server) compactDownload(c fiber.Ctx, job jobs.Job) error {
	if c.Get("HX-Request") != "true" {
		return c.Redirect().Status(303).To(fmt.Sprintf("/collections/%s?tab=downloads&download_job=%s#download", job.Resource, job.ID))
	}
	p := s.base(c, "Download", "collections")
	p.Jobs = []jobs.Job{job}
	return c.Render("partials/download-job", p)
}

func (s *Server) downloadError(c fiber.Ctx, err error) error {
	if c.FormValue("download_view") != "compact" {
		return fiber.NewError(422, err.Error())
	}
	if c.Get("HX-Request") != "true" {
		if job, e := s.Service.Jobs.Get(c.Params("id")); e == nil && job.Resource != "" {
			return c.Redirect().Status(303).To(fmt.Sprintf("/collections/%s?download_job=%s#download", job.Resource, job.ID))
		}
		c.Locals("download_error", err.Error())
		return s.detail(c)
	}
	p := s.base(c, "Download", "collections")
	p.DownloadError = err.Error()
	// A successful fragment response lets HTMX show the actionable error inline.
	return c.Render("partials/download-job", p)
}
