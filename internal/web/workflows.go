package web

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"strings"
	"time"
)

// Only multipart file parts bypass the ordinary form ceiling. CSRF can be the
// first part for native forms, or the header used by HTMX.
func (s *Server) boundForms(c fiber.Ctx) error {
	if c.Method() == "GET" || c.Method() == "HEAD" {
		return c.Next()
	}
	streamed := c.Path() == "/collections/import" || c.Path() == "/converter"
	media, params, _ := mime.ParseMediaType(c.Get("Content-Type"))
	if streamed && media == "multipart/form-data" {
		var body io.Reader = c.Request().BodyStream()
		if body == nil {
			body = bytes.NewReader(c.Body())
		}
		r := multipart.NewReader(body, params["boundary"])
		if c.Get("X-CSRF-Token") == "" {
			p, e := r.NextPart()
			if e != nil || p.FormName() != "csrf" || p.FileName() != "" {
				return fiber.NewError(403, "CSRF must be the first upload field.")
			}
			b, e := io.ReadAll(io.LimitReader(p, 257))
			if e != nil || len(b) > 256 {
				return fiber.ErrBadRequest
			}
			p.Close()
			c.Locals("multipart_csrf", string(b))
		}
		c.Locals("multipart_reader", r)
		return c.Next()
	}
	if c.Request().IsBodyStream() {
		b, e := io.ReadAll(io.LimitReader(c.Request().BodyStream(), (1<<20)+1))
		if e != nil {
			return e
		}
		if len(b) > 1<<20 {
			return fiber.NewError(413, "Ordinary forms are limited to 1 MiB.")
		}
		c.Request().SetBody(b)
	} else if len(c.Body()) > 1<<20 {
		return fiber.NewError(413, "Ordinary forms are limited to 1 MiB.")
	}
	return c.Next()
}

type UploadResult struct {
	Name, State, Error string
}

func (s *Server) importFile(c fiber.Ctx) error {
	r, ok := c.Locals("multipart_reader").(*multipart.Reader)
	if !ok {
		return fiber.NewError(400, "Choose files to import.")
	}
	count := 0
	results := []UploadResult{}
	for parts := 0; parts < 200; parts++ {
		p, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fiber.NewError(400, "Upload interrupted: "+e.Error())
		}
		if p.FileName() == "" {
			field, readErr := io.ReadAll(io.LimitReader(p, 65537))
			e = readErr
			p.Close()
			if e != nil {
				return e
			}
			if len(field) > 65536 {
				return fiber.NewError(413, "Upload form field exceeds 64 KiB.")
			}
			continue
		}
		count++
		if count > 100 {
			return fiber.NewError(400, "Import at most 100 files per batch.")
		}
		col, e := s.Service.Import(p.FileName(), p)
		result := UploadResult{Name: p.FileName(), State: "processing"}
		if e != nil {
			result.State, result.Error = "rejected", e.Error()
			if _, err := s.Service.Jobs.Failed(p.FileName(), e); err != nil {
				return err
			}
		} else if col.Status == "ready" {
			result.State = "ready"
		}
		results = append(results, result)
		p.Close()
	}
	if count == 0 {
		return fiber.NewError(400, "Choose files to import.")
	}
	c.Set("HX-Trigger", "collectionsChanged")
	if c.Get("HX-Request") == "true" {
		page := s.base(c, "Processing", "collections")
		page.UploadResults = results
		var err error
		page.Jobs, err = s.Service.Jobs.List()
		if err != nil {
			return err
		}
		return c.Render("partials/jobs", page)
	}
	return c.Redirect().Status(303).To("/collections")
}

func tableRows(rows []any, fields []collections.Field) ([]string, [][]string) {
	headers := []string{}
	names := []string{}
	seen := map[string]bool{}
	available := map[string]bool{}
	for _, f := range fields {
		available[f.Name] = true
	}
	for _, name := range []string{"id", "name", "email", "sku", "status", "event_type", "timestamp"} {
		if available[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	for _, f := range fields {
		if !seen[f.Name] {
			names = append(names, f.Name)
		}
	}
	for _, name := range names {
		if len(headers) == 48 {
			break
		}
		visible := false
		for _, v := range rows {
			if m, ok := v.(map[string]any); ok {
				value, exists := m[name]
				if exists {
					if text, isString := value.(string); !isString || text != "" {
						visible = true
						break
					}
				}
			}
		}
		if visible {
			headers = append(headers, name)
		}
	}
	if len(headers) == 0 {
		return nil, nil
	}
	cells := [][]string{}
	for _, v := range rows {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, nil
		}
		row := []string{}
		for _, k := range headers {
			value, exists := m[k]
			text := "—"
			if exists {
				if s, ok := value.(string); ok {
					text = s
				} else {
					b, _ := conversion.MarshalJSON(value)
					text = string(b)
				}
			}
			runes := []rune(text)
			if len(runes) > 512 {
				text = string(runes[:512]) + "…"
			}
			row = append(row, text)
		}
		cells = append(cells, row)
	}
	return headers, cells
}

func (s *Server) jobs(c fiber.Ctx) error {
	if c.Query("download_view") == "compact" {
		job, err := s.Service.Jobs.Get(c.Query("id"))
		if err != nil || job.Kind != "convert" {
			return fiber.NewError(404, "Download not found")
		}
		return s.compactDownload(c, job)
	}
	p := s.base(c, "Processing", "collections")
	var err error
	p.Jobs, err = s.Service.Jobs.List()
	if err != nil {
		return err
	}
	return c.Render("partials/jobs", p)
}
func (s *Server) cancelJob(c fiber.Ctx) error {
	if err := s.Service.Jobs.Cancel(c.Params("id")); err != nil {
		return err
	}
	if c.FormValue("download_view") == "compact" {
		job, err := s.Service.Jobs.Get(c.Params("id"))
		if err != nil {
			return err
		}
		return s.compactDownload(c, job)
	}
	return s.jobs(c)
}
func (s *Server) retryJob(c fiber.Ctx) error {
	job, err := s.Service.RetryJob(c.Params("id"))
	if err != nil {
		return s.downloadError(c, err)
	}
	if c.FormValue("download_view") == "compact" {
		return s.compactDownload(c, job)
	}
	return s.jobs(c)
}
func (s *Server) download(c fiber.Ctx) error {
	path, err := s.Service.Download(c.Params("id"))
	if err != nil {
		return fiber.NewError(404, err.Error())
	}
	c.Attachment(path)
	// Disposable outputs must not keep cached file handles after a download.
	return c.SendFile(path, fiber.SendFile{CacheDuration: -1})
}
func (s *Server) events(c fiber.Ctx) error {
	ch, close, err := s.Service.Jobs.Subscribe()
	if err != nil {
		return fiber.NewError(503, err.Error())
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer close()
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		if _, e := fmt.Fprint(w, "event: progress\ndata: connected\n\n"); e != nil {
			return
		}
		if e := w.Flush(); e != nil {
			return
		}
		for {
			select {
			case id := <-ch:
				if id == "*" {
					if _, e := fmt.Fprint(w, "event: progress\ndata: {\"resource\":\"*\",\"state\":\"changed\"}\n\n"); e != nil {
						return
					}
					break
				}
				job, e := s.Service.Jobs.Get(id)
				if e != nil {
					continue
				}
				data, _ := json.Marshal(map[string]string{"id": id, "resource": job.Resource, "state": job.State})
				if _, e := fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data); e != nil {
					return
				}
			case <-tick.C:
				if _, e := fmt.Fprint(w, ": keepalive\n\n"); e != nil {
					return
				}
			}
			if e := w.Flush(); e != nil {
				return
			}
		}
	})
}
func (s *Server) demo(c fiber.Ctx) error {
	if err := s.Service.Demo(); err != nil {
		return err
	}
	c.Set("HX-Trigger", "collectionsChanged")
	if c.Get("HX-Request") == "true" {
		return s.jobs(c)
	}
	return c.Redirect().Status(303).To("/collections")
}
func (s *Server) excluded(c fiber.Ctx) error {
	p := s.base(c, "Excluded files", "collections")
	var err error
	p.Excluded, err = s.Service.Excluded()
	if err != nil {
		return err
	}
	return c.Render("pages/excluded", p, "layouts/base")
}
func (s *Server) restore(c fiber.Ctx) error {
	if err := s.Service.Restore(c.Params("key")); err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Redirect().Status(303).To("/excluded")
}
func (s *Server) remove(c fiber.Ctx) error {
	if err := s.Service.Remove(c.Params("id")); err != nil {
		return err
	}
	if c.Get("HX-Request") == "true" {
		return s.collectionList(c)
	}
	return c.Redirect().Status(303).To("/collections")
}
func (s *Server) rename(c fiber.Ctx) error {
	if err := s.Service.Rename(c.Params("id"), c.FormValue("name")); err != nil {
		return fiber.NewError(422, err.Error())
	}
	c.Set("HX-Redirect", "/collections/"+c.Params("id"))
	return c.Redirect().Status(303).To("/collections/" + c.Params("id"))
}
func (s *Server) refreshOne(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	if err = s.Service.SyncFile(col.Path); err != nil {
		return err
	}
	c.Set("HX-Trigger", "collectionsChanged")
	return s.jobs(c)
}
func (s *Server) exportJob(c fiber.Ctx) error {
	if c.FormValue("action") == "review" {
		return s.reviewDownload(c)
	}
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	if c.FormValue("format") == "original" && c.FormValue("download_view") == "compact" {
		if c.FormValue("pipeline") != "" {
			return s.downloadError(c, fmt.Errorf("Choose an output format to download saved changes"))
		}
		return s.originalDownload(c, col)
	}
	var job jobs.Job
	if id := c.FormValue("pipeline"); id != "" && c.FormValue("download_view") == "compact" {
		recipe, e := s.Service.GetPipeline(id)
		if e != nil || recipe.Source != col.ID {
			return s.downloadError(c, fmt.Errorf("Saved steps not found for this dataset"))
		}
		job, err = s.Service.PipelineJob(recipe, c.FormValue("format", "json"))
	} else {
		job, err = s.Service.ExportJobWithOptions(col.ID, c.FormValue("format", col.Format), conversion.Options{SpreadsheetSafe: c.FormValue("spreadsheet_safe") == "on"})
	}
	if err != nil {
		return s.downloadError(c, err)
	}
	if c.FormValue("download_view") == "compact" {
		return s.compactDownload(c, job)
	}
	return s.jobs(c)
}

// File conversions are staged; small input keeps its existing synchronous UI.
func (s *Server) multipartConvert(c fiber.Ctx) error {
	r := c.Locals("multipart_reader").(*multipart.Reader)
	values := map[string]string{}
	path := ""
	keep := false
	defer func() {
		if path != "" && !keep {
			os.Remove(path)
		}
	}()
	for n := 0; n < 10; n++ {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fiber.ErrBadRequest
		}
		if p.FileName() != "" {
			if path != "" {
				return fiber.ErrBadRequest
			}
			path, err = s.Service.StageConversion(p.FileName(), p)
			if err != nil {
				return fiber.NewError(422, err.Error())
			}

		} else {
			limit := int64(65536)
			if p.FormName() == "input" {
				limit = s.Service.MaxBytes
			}
			b, e := io.ReadAll(io.LimitReader(p, limit+1))
			if e != nil || int64(len(b)) > limit {
				return fiber.NewError(413, "Input exceeds document limit.")
			}
			if p.FormName() == "input_mode" || p.FormName() == "collection_id" || p.FormName() == "input" || p.FormName() == "from" || p.FormName() == "to" || p.FormName() == "infer_types" || p.FormName() == "csv_delimiter" || p.FormName() == "spreadsheet_safe" || p.FormName() == "column_types" || p.FormName() == "parse_json" || p.FormName() == "roundtrip" {
				values[p.FormName()] = string(b)
			}
		}
		p.Close()
	}
	columns, e := conversion.ParseColumnTypes(values["column_types"])
	if e != nil {
		return fiber.NewError(422, e.Error())
	}
	c.Locals("column_rules", values["column_types"])
	c.Locals("roundtrip", values["roundtrip"] == "on")
	c.Locals("conversion_options", conversion.Options{CSVInferTypes: values["infer_types"] == "on", CSVDelimiter: defaultFormat(values["csv_delimiter"], "auto"), SpreadsheetSafe: values["spreadsheet_safe"] == "on", CSVColumnTypes: columns, CSVParseJSON: values["parse_json"] == "on"})
	if values["input_mode"] == "collection" {
		return s.convertCollection(c, values["collection_id"], values["to"])
	}
	if path == "" {
		return s.convertValues(c, []byte(values["input"]), values["from"], values["to"])
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= s.Service.MaxBytes {
		defer os.Remove(path)
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return s.convertValues(c, b, values["from"], values["to"])
	}
	if _, err = s.Service.ConvertJobWithOptions(path, defaultFormat(values["from"], "json"), defaultFormat(values["to"], "yaml"), converterOptions(c)); err != nil {
		return fiber.NewError(422, err.Error())
	}
	keep = true
	if c.Get("HX-Request") != "true" {
		return c.Redirect().Status(303).To("/converter#activity")
	}
	c.Set("HX-Trigger", "jobQueued")
	p := s.base(c, "Format converter", "converter")
	p.Message = "Conversion queued. Follow progress and download the result in File activity."
	return c.Render("partials/queued", p)
}

func defaultFormat(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func converterOptions(c fiber.Ctx) conversion.Options {
	if options, ok := c.Locals("conversion_options").(conversion.Options); ok {
		return options
	}
	columns, _ := conversion.ParseColumnTypes(c.FormValue("column_types"))
	return conversion.Options{CSVInferTypes: c.FormValue("infer_types") == "on", CSVDelimiter: defaultFormat(c.FormValue("csv_delimiter"), "auto"), SpreadsheetSafe: c.FormValue("spreadsheet_safe") == "on", CSVColumnTypes: columns, CSVParseJSON: c.FormValue("parse_json") == "on"}
}
