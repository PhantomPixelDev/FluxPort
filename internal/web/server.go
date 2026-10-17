package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"fluxport/internal/api"
	"fluxport/internal/apipolicy"
	"fluxport/internal/auth"
	"fluxport/internal/collections"
	"fluxport/internal/config"
	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/ui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"
	htmlengine "github.com/gofiber/template/html/v3"
)

type Server struct {
	APIPolicy *apipolicy.Manager
	Config    config.Config
	Service   *collections.Service
	Auth      *auth.Auth
}
type Page struct {
	APIOptions                                                                             apipolicy.Policy
	APICollection                                                                          apipolicy.Collection
	APIOrigins                                                                             string
	APISettingsMessage                                                                     string
	FileHistory                                                                            *collections.FileHistory
	FileJob                                                                                *jobs.Job
	FileEditable                                                                           bool
	InfoMessage                                                                            string
	WorkspaceView                                                                          string
	Original                                                                               *Page
	PreparedJob                                                                            *jobs.Job
	Prepared                                                                               *collections.PreparedResult
	ResultID                                                                               string
	JobsResource                                                                           string
	RawQueryJSON                                                                           string
	DownloadReview                                                                         *DownloadReview
	Insights                                                                               *DatasetInsights
	DatasetFormats                                                                         []string
	Quality                                                                                *QualityPage
	Pipeline                                                                               *PipelinePage
	SQL                                                                                    *SQLPage
	Workbench                                                                              *Workbench
	AssetVersion                                                                           string
	InputMode, CollectionID                                                                string
	Limit                                                                                  int
	UploadResults                                                                          []UploadResult
	RoundTrip, ParseJSON                                                                   bool
	ColumnRules, ConversionSummary                                                         string
	InputTree, OutputTree                                                                  []conversion.TreeNode
	Differences                                                                            []string
	Fragment                                                                               bool
	OutputReady, InferTypes, SpreadsheetSafe, ConverterSubmitted                           bool
	Input, InputFormat, CSVDelimiter                                                       string
	ConversionNotes                                                                        []string
	CSVDelimiters                                                                          []string
	Jobs                                                                                   []jobs.Job
	DownloadOpen                                                                           bool
	DownloadError                                                                          string
	DownloadFormat                                                                         string
	DownloadDraft                                                                          string
	Excluded                                                                               []collections.Excluded
	Headers                                                                                []string
	Cells                                                                                  [][]string
	Issues                                                                                 int
	Title, Active, CSRF, Query, Tab, Message, Output, OutputFormat, Preview, Root, Address string
	Collections                                                                            []collections.Collection
	Collection                                                                             collections.Collection
	Fields                                                                                 []collections.Field
	Formats                                                                                []string
	Page, Prev, Next, Pages, Total, Count, Enabled, Rows                                   int
	Size                                                                                   int64
	Watch, Authenticated, APIProtected                                                     bool
	MaxBytes                                                                               int64
}

func New(cfg config.Config, s *collections.Service) (*fiber.App, error) {
	if cfg.PublicDemo {
		cfg.APIKey = ""
	}
	a, err := auth.New(cfg.AdminPassword)
	if err != nil {
		return nil, err
	}
	policy, err := apipolicy.New(s.DB, cfg.TrustedProxies...)
	if err != nil {
		return nil, err
	}
	server := &Server{Config: cfg, Service: s, Auth: a, APIPolicy: policy}
	templates, err := fs.Sub(ui.Files, "templates")
	if err != nil {
		return nil, err
	}
	engine := htmlengine.NewFileSystem(http.FS(templates), ".html")
	engine.AddFunc("bytes", func(n int64) string {
		if n < 1024 {
			return fmt.Sprintf("%d B", n)
		}
		if n < 1<<20 {
			return fmt.Sprintf("%.1f KB", float64(n)/1024)
		}
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	})
	engine.AddFunc("upper", strings.ToUpper)
	engine.AddFunc("inc", func(n int) int { return n + 1 })
	engine.AddFunc("formatName", downloadFormatName)
	engine.AddFunc("formatNote", downloadFormatNote)
	engine.AddFunc("jobFilename", server.jobDownloadFilename)
	engine.AddFunc("jobFormat", func(j jobs.Job) string {
		f := downloadJobFormat(j)
		if f == "" {
			return ""
		}
		return downloadFormatName(f)
	})
	engine.AddFunc("stepName", cleanupStepName)
	engine.AddFunc("stepJSON", cleanupStepJSON)
	engine.AddFunc("rowNumber", func(n int64) int64 { return n + 1 })

	engine.AddFunc("fieldType", func(t string) string {
		switch t {
		case "Boolean":
			return "checkbox"
		case "Number":
			return "number"
		case "JSON":
			return "json"
		default:
			return "text"
		}
	})

	engine.AddFunc("jobState", func(state string) string {
		switch state {
		case "staged":
			return "Uploaded"
		case "queued", "running", "processing":
			return "Working"
		case "ready":
			return "Done"
		case "failed":
			return "Failed"
		case "cancelled", "interrupted":
			return "Cancelled"
		default:
			return state
		}
	})
	engine.AddFunc("has", func(s string) bool { return s != "" })
	if err = engine.Load(); err != nil {
		return nil, err
	}
	// SSE and large downloads must not inherit a short absolute write deadline.
	app := fiber.New(fiber.Config{AppName: "FluxPort", Views: engine, BodyLimit: int(cfg.MaxFileBytes) + (1 << 20), StreamRequestBody: true, DisablePreParseMultipartForm: true, ReadTimeout: 30 * time.Minute, WriteTimeout: 0, IdleTimeout: 60 * time.Second, ErrorHandler: server.errorHandler})
	app.Use(recover.New())
	app.Use(compress.New(compress.Config{Next: func(c fiber.Ctx) bool { return c.Path() == "/events" }}))
	app.Use(func(c fiber.Ctx) error {

		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "same-origin")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if !strings.HasPrefix(c.Path(), "/static/") {
			c.Set("Cache-Control", "no-store")
		}
		host, _, _ := net.SplitHostPort(cfg.Address)
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() {
			h := c.Hostname()
			if h != "localhost" && h != "127.0.0.1" && h != "::1" && h != "[::1]" {
				return fiber.NewError(403, "Unrecognized loopback host.")
			}
		}
		return c.Next()
	})
	// Bound ordinary request work; SSE connections have their own subscription limit.
	permits := make(chan struct{}, 4)
	app.Use(func(c fiber.Ctx) error {
		if c.Path() == "/events" || strings.HasPrefix(c.Path(), "/static/") || c.Path() == "/healthz" {
			return c.Next()
		}
		select {
		case permits <- struct{}{}:
			defer func() { <-permits }()
			return c.Next()
		default:
			return fiber.NewError(503, "Server is busy. Try again shortly.")
		}
	})
	app.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/static/*", server.static)
	app.Use(server.boundForms)
	app.Use(a.Middleware)
	app.Get("/login", func(c fiber.Ctx) error { return server.render(c, "login", "Sign in", nil) })
	app.Post("/login", limiter.New(limiter.Config{Max: 5, Expiration: time.Minute}), a.Login)
	app.Post("/logout", a.Logout)
	app.Get("/", server.collectionList)
	app.Get("/overview", server.overview)
	app.Get("/collections", server.collectionList)
	app.Post("/collections/import", server.importFile)
	app.Post("/collections/demo", server.demo)
	app.Get("/excluded", server.excluded)
	app.Post("/excluded/:key/restore", server.restore)
	app.Get("/jobs", server.jobs)
	app.Get("/events", server.events)
	app.Post("/jobs/:id/cancel", server.cancelJob)
	app.Post("/jobs/:id/retry", server.retryJob)
	app.Get("/jobs/:id/download", server.download)
	app.Post("/collections/refresh", server.refresh)
	app.Get("/collections/:id/export", server.export)
	app.Post("/collections/:id/export", server.exportJob)
	app.Get("/collections/:id", server.detail)
	app.Get("/collections/:id/workbench", server.workbench)
	app.Post("/collections/:id/cleaned-results", server.prepareCleanedResult)
	app.Get("/collections/:id/cleaned-results/:result", server.cleanedResultStatus)
	app.Get("/collections/:id/changes", server.datasetChanges)
	app.Post("/collections/:id/changes", server.datasetChanges)
	app.Post("/collections/:id/export-check", server.reviewDownload)
	app.Get("/collections/:id/export-check", server.reviewDownload)
	app.Post("/collections/:id/workbench/enable", server.enableDatasetEditing)
	app.Post("/collections/:id/sheet", server.selectWorkbookSheet)
	app.Get("/collections/:id/quality", server.qualityPage)
	app.Post("/pipelines/:pipeline/audit", server.pipelineAudit)
	app.Get("/collections/:id/insights", server.datasetInsights)
	app.Post("/collections/:id/quality", server.qualityPage)
	app.Get("/collections/:id/workbench/status", server.workbenchStatus)
	app.Post("/collections/:id/workbench/edit", server.editRecords)
	app.Post("/collections/:id/editable-copy", server.editableCopy)
	app.Get("/collections/:id/editable-copy/:job", server.editableCopyStatus)
	app.Get("/collections/:id/file-history", server.fileHistory)
	app.Post("/collections/:id/file-history", server.restoreFile)
	app.Post("/collections/:id/workbench/export", server.exportWorkbench)
	app.Post("/collections/:id/workbench/views", server.saveWorkbenchView)
	app.Post("/collections/:id/workbench/views/:view/delete", server.deleteWorkbenchView)
	app.Post("/collections/:id/config", server.update)
	app.Post("/collections/:id/toggle", server.toggle)
	app.Post("/collections/:id/rename", server.rename)
	app.Post("/collections/:id/refresh", server.refreshOne)
	app.Post("/collections/:id/remove", server.remove)
	app.Post("/collections/:id/delete", server.delete)
	app.Get("/converter", server.converter)
	app.Get("/converter/collections", server.converterCollections)
	app.Get("/workflow", server.workflow)
	app.Get("/pipelines", server.pipelines)
	app.Post("/pipelines", server.pipelines)
	app.Post("/pipelines/:pipeline/delete", server.deletePipeline)
	app.Post("/pipelines/:pipeline/restore", server.restorePipeline)
	app.Post("/pipelines/:pipeline/schedule", server.schedulePipeline)
	app.Get("/pipelines/:pipeline/export", server.exportPipeline)
	app.Get("/sql", server.sqlPage)
	app.Get("/collections/:id/sql", server.sqlPage)
	app.Post("/collections/:id/sql", server.sqlPage)
	app.Post("/sql", server.sqlPage)
	app.Post("/sql/:query/delete", server.deleteSQL)
	app.Post("/converter", server.convert)
	app.Get("/explorer", server.explorer)
	app.Get("/settings", server.settings)
	app.Post("/settings", server.saveSettings)
	app.Post("/settings/api", server.saveAPISettings)
	app.Post("/collections/:id/api-settings", server.saveCollectionAPISettings)
	(&api.API{Service: s, Key: cfg.APIKey, Policy: policy}).Register(app)
	app.Use(func(c fiber.Ctx) error { return fiber.NewError(404, "Page not found.") })
	return app, nil
}
func (s *Server) base(c fiber.Ctx, title, active string) Page {
	return Page{AssetVersion: frontendVersion, Title: title, Active: active, CSRF: auth.CSRF(c), Formats: conversion.Formats, DatasetFormats: collections.ExportFormats(), CSVDelimiters: []string{"comma", "semicolon", "tab", "pipe"}, Root: s.Service.Root, MaxBytes: s.Config.MaxFileBytes, Address: s.Config.Address, Authenticated: s.Config.AdminPassword != "", APIProtected: s.Config.APIKey != ""}
}

// Content-based URLs let an existing browser pick up UI updates immediately
// while retaining normal asset caching between releases.
var frontendVersion = func() string {
	version, err := frontendAssetVersion(ui.Files)
	if err != nil {
		panic(err)
	}
	return version
}()

func frontendAssetVersion(files fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(files, "static", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(b)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

func (s *Server) render(c fiber.Ctx, name, title string, fill func(*Page)) error {
	p := s.base(c, title, name)
	if fill != nil {
		fill(&p)
	}
	return c.Render("pages/"+name, p, "layouts/base")
}
func (s *Server) errorHandler(c fiber.Ctx, err error) error {
	status := 500
	message := "An unexpected server error occurred."
	var fe *fiber.Error
	if errors.As(err, &fe) {
		status = fe.Code
		message = fe.Message
	} else if errors.Is(err, sql.ErrNoRows) {
		status = 404
		message = "Collection not found."
	} else {
		slog.Error("request failed", "path", c.Path(), "error", err)
	}
	c.Status(status)
	if strings.HasPrefix(c.Path(), "/api/") {
		return c.JSON(fiber.Map{"error": message, "status": status})
	}
	p := s.base(c, "Request failed", "error")
	p.Message = message
	if c.Get("HX-Request") == "true" || c.Get("X-Fluxport-View") == "workspace" {
		return c.Render("partials/error", p)
	}
	return c.Render("pages/error", p, "layouts/base")
}
func (s *Server) static(c fiber.Ctx) error {
	name := path.Clean(c.Params("*"))
	b, err := ui.Files.ReadFile("static/" + name)
	if err != nil {
		return fiber.ErrNotFound
	}
	h := sha256.Sum256(b)
	etag := "\"" + hex.EncodeToString(h[:12]) + "\""
	c.Set("ETag", etag)
	c.Set("Cache-Control", "public, max-age=3600")
	if c.Get("If-None-Match") == etag {
		return c.SendStatus(304)
	}
	switch path.Ext(name) {
	case ".css":
		c.Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		c.Set("Content-Type", "text/javascript; charset=utf-8")
	case ".svg":
		c.Set("Content-Type", "image/svg+xml")
	}
	return c.Send(b)
}
func (s *Server) listData(c fiber.Ctx) (Page, error) {
	p := s.base(c, "Files", "collections")
	p.Query = c.Query("q")
	if c.Method() == "POST" {
		p.Query = c.FormValue("q")
	}
	p.Page = 1
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n > 0 && n <= 1000000 {
		p.Page = n
	}
	items, total, err := s.Service.List(p.Query, p.Page, 20)
	if err != nil {
		return p, err
	}
	p.Collections = items
	p.Total = total
	p.Pages = (total + 19) / 20
	if p.Pages < 1 {
		p.Pages = 1
	}
	if p.Page > p.Pages {
		p.Page = p.Pages
		p.Collections, _, err = s.Service.List(p.Query, p.Page, 20)
	}
	if p.Page > 1 {
		p.Prev = p.Page - 1
	}
	if p.Page < p.Pages {
		p.Next = p.Page + 1
	}
	return p, err
}
func (s *Server) overview(c fiber.Ctx) error {
	count, enabled, rows, size, err := s.Service.Stats()
	if err != nil {
		return err
	}
	items, _, err := s.Service.List("", 1, 6)
	if err != nil {
		return err
	}
	return s.render(c, "overview", "Overview", func(p *Page) {
		p.Count = count
		p.Enabled = enabled
		p.Rows = rows
		p.Size = size
		p.Collections = items
		p.Watch = s.Service.Setting("watch_enabled", "true") == "true"
		p.Issues = s.Service.IssueCount()
	})
}
func (s *Server) collectionList(c fiber.Ctx) error {
	p, err := s.listData(c)
	if err != nil {
		return err
	}
	if c.Get("HX-Request") == "true" {
		return c.Render("partials/collections", p)
	}
	return c.Render("pages/collections", p, "layouts/base")
}
func (s *Server) refresh(c fiber.Ctx) error {
	if err := s.Service.Sync(); err != nil {
		return err
	}
	return s.collectionList(c)
}
func (s *Server) detail(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	p := s.base(c, col.Name, "collections")
	p.Collection = col
	if resultID := c.Query("result_id"); resultID != "" && (c.Query("tab", "preview") == "preview" || c.Query("tab") == "raw") {
		p, err = s.resultPage(c, col.ID, resultID)
		if err != nil {
			return err
		}
		p.Title = col.Name
		if c.Query("tab") == "raw" && p.Workbench != nil {
			values := []string{}
			for _, row := range p.Workbench.Rows {
				values = append(values, row.JSON)
			}
			p.Preview = "[" + strings.Join(values, ",") + "]"
			p.RawQueryJSON = p.Workbench.QueryJSON
			p.Workbench = nil
			p.PreparedJob = nil
			p.Tab = "raw"
		}
		if c.Get("HX-Request") == "true" && c.Get("X-Fluxport-View") != "workspace" {
			return c.Render("partials/detail", p)
		}
		return s.renderCollectionView(c, detailWorkspaceView(p.Tab), p)
	}
	if message, ok := c.Locals("download_error").(string); ok {
		p.DownloadError = message
		p.DownloadOpen = true
		p.DownloadFormat = c.FormValue("format")
		if c.FormValue("dataset") == "current" {
			p.DownloadDraft = c.FormValue("draft")
		}
	}
	if id := c.Query("download_job"); id != "" {
		job, e := s.Service.Jobs.Get(id)
		if e != nil || job.Resource != col.ID || job.Kind != "convert" {
			return fiber.NewError(404, "Download not found")
		}
		p.Jobs = []jobs.Job{job}
		p.DownloadFormat = downloadJobFormat(job)
		p.DownloadDraft = downloadJobDraft(job)
		p.ResultID = downloadJobResult(job)
		p.DownloadOpen = true
	}
	p.Query = strings.TrimSpace(c.Query("q"))
	p.Pages = 1
	if len(p.Query) > 256 {
		return fiber.NewError(400, "Search is limited to 256 characters.")
	}
	p.Limit = 20
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && (n == 20 || n == 50 || n == 100) {
		p.Limit = n
	}
	p.Tab = c.Query("tab", "preview")
	if p.DownloadError != "" {
		p.Tab = "downloads"
	}
	switch p.Tab {
	case "preview", "raw", "schema", "configuration", "downloads":
	default:
		p.Tab = "preview"
	}
	if p.Tab == "preview" {
		if err := s.fillWorkbench(c, &p, col); err != nil {
			return err
		}
		if c.Get("HX-Request") == "true" && c.Get("X-Fluxport-View") != "workspace" {
			p.Fragment = true
			return c.Render("partials/detail", p)
		}
		return s.renderCollectionView(c, detailWorkspaceView(p.Tab), p)
	}
	if p.Tab == "downloads" {
		q, e := decodeQuery(c.Query("query", c.FormValue("query")))
		if e != nil {
			return fiber.NewError(400, e.Error())
		}
		b, _ := json.Marshal(q)
		p.Workbench = &Workbench{Query: q, QueryJSON: string(b), Generation: col.Generation, ResultID: c.Query("result_id", c.FormValue("result_id", p.ResultID)), DatasetRows: col.Rows}
		p.ResultID = p.Workbench.ResultID
		if p.ResultID != "" {
			job, e := s.Service.Jobs.Get(p.ResultID)
			if e != nil || job.Resource != col.ID || job.Kind != "prepare_result" {
				return fiber.NewError(404, "Cleaned result not found")
			}
			if job.State == "ready" {
				if d, e := s.Service.PreparedResult(col.ID, p.ResultID); e == nil {
					p.Workbench.DatasetRows = d.Rows
				}
			}
		}
		if p.DownloadFormat == "" && (c.Query("query") != "" || p.ResultID != "") {
			p.DownloadFormat = col.Format
			if p.ResultID != "" {
				p.DownloadFormat = "json"
			}
		}
		p.Pipeline = &PipelinePage{}
		all, e := s.Service.Pipelines()
		if e != nil {
			return e
		}
		for _, recipe := range all {
			if recipe.Source == col.ID {
				p.Pipeline.Saved = append(p.Pipeline.Saved, recipe)
			}
		}
		id := c.Query("steps")
		if id == "" && p.DownloadError != "" {
			id = c.FormValue("dataset", c.FormValue("pipeline"))
			if id == "original" || id == "current" {
				id = ""
			}
		}
		if id == "" && len(p.Jobs) > 0 {
			id = downloadJobSteps(p.Jobs[0])
		}
		if id != "" {
			recipe, e := s.Service.GetPipeline(id)
			if e != nil || recipe.Source != col.ID {
				if len(p.Jobs) > 0 || p.DownloadError != "" {
					return s.renderCollectionView(c, detailWorkspaceView(p.Tab), p)
				}
				return fiber.NewError(404, "Saved steps not found for this dataset")
			}
			p.Pipeline.Definition = recipe
		}
		return s.renderCollectionView(c, detailWorkspaceView(p.Tab), p)
	}
	p.Page = 1
	if n, e := strconv.Atoi(c.Query("page")); e == nil && n > 0 && n <= 1000000000 {
		p.Page = n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	rows, total, readErr := s.Service.Page(ctx, col, p.Page, p.Limit, false, p.Query)
	if readErr != nil {
		p.Message = readErr.Error()
	} else {
		p.Fields, _ = s.Service.Fields(col)
		p.Total = int(total)
		p.Pages = max(1, (p.Total+p.Limit-1)/p.Limit)
		if p.Page > 1 {
			p.Prev = p.Page - 1
		}
		if p.Page < p.Pages {
			p.Next = p.Page + 1
		}
		b, _ := conversion.Encode(rows, "json")
		if len(b) <= 65536 {
			p.Preview = string(b)
		} else {
			p.Preview = "Page exceeds 64 KB; use the API to download it."
		}
		p.Headers, p.Cells = tableRows(rows, p.Fields)
		p.Collection, err = s.Service.Get(col.ID)
		if err != nil {
			return err
		}
	}
	if c.Get("HX-Request") == "true" && c.Get("X-Fluxport-View") != "workspace" {
		p.Fragment = true
		return c.Render("partials/detail", p)
	}
	return s.renderCollectionView(c, detailWorkspaceView(p.Tab), p)
}
func (s *Server) update(c fiber.Ctx) error {
	rules := strings.TrimSpace(c.FormValue("rules"))
	crud := c.FormValue("crud") == "on"
	if crud && rules != "" && rules != "{}" {
		return fiber.NewError(422, "CRUD requires transformations to be empty.")
	}
	if err := s.Service.Update(c.Params("id"), c.FormValue("enabled") == "on", crud, rules); err != nil {
		return fiber.NewError(422, err.Error())
	}
	if c.Get("HX-Request") == "true" {
		p := s.base(c, "", "collections")
		p.Message = "Endpoint configuration saved."
		col, err := s.Service.Get(c.Params("id"))
		if err != nil {
			return err
		}
		p.Collection = col
		return c.Render("partials/success", p)
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id") + "?tab=configuration")
}
func (s *Server) toggle(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	if err = s.Service.Update(col.ID, !col.Enabled, col.CRUD, col.Rules); err != nil {
		return err
	}
	return s.collectionList(c)
}
func (s *Server) delete(c fiber.Ctx) error {
	if c.FormValue("confirm") != "delete" {
		return fiber.NewError(422, "Confirm permanent source-file deletion before continuing.")
	}
	if err := s.Service.Delete(c.Params("id")); err != nil {
		return err
	}
	if c.Get("HX-Request") == "true" {
		return s.collectionList(c)
	}
	return c.Redirect().Status(303).To("/collections")
}
func (s *Server) export(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	if c.Query("original") == "1" {
		return s.originalDownload(c, col)
	}
	if col.Size > s.Service.MaxBytes {
		f := conversion.Format(c.Query("format", col.Format))
		if f != col.Format {
			return fiber.NewError(422, "Choose Prepare download to convert large collections.")
		}
		r, e := s.Service.RawSource(col)
		if e != nil {
			return e
		}
		c.Set("Content-Type", conversion.MIME(f))
		c.Set("Content-Disposition", "attachment; filename=\"collection."+f+"\"")
		return c.SendStream(r, int(col.Size))
	}
	v, err := s.Service.Read(col)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	f := conversion.Format(c.Query("format", col.Format))
	b, err := conversion.Encode(v, f)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	c.Set("Content-Type", conversion.MIME(f))
	c.Set("Content-Disposition", "attachment; filename=\"collection."+f+"\"")
	return c.Send(b)
}
func (s *Server) convert(c fiber.Ctx) error {
	if c.Locals("multipart_reader") != nil {
		return s.multipartConvert(c)
	}
	if c.FormValue("input_mode") == "collection" {
		return s.convertCollection(c, c.FormValue("collection_id"), c.FormValue("to"))
	}
	input := []byte(c.FormValue("input"))
	if f, err := c.FormFile("file"); err == nil && f.Size > 0 {
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		input, err = io.ReadAll(io.LimitReader(r, s.Config.MaxFileBytes+1))
		if err != nil {
			return err
		}
	}
	if int64(len(input)) > s.Config.MaxFileBytes {
		return fiber.NewError(413, "Input exceeds configured size limit.")
	}
	return s.convertValues(c, input, c.FormValue("from", "json"), c.FormValue("to", "yaml"))
}
func (s *Server) convertValues(c fiber.Ctx, input []byte, from, to string) error {
	options := converterOptions(c)
	p := s.base(c, "Format converter", "converter")
	if err := s.fillConverter(c, &p); err != nil {
		return err
	}
	p.ConverterSubmitted = true
	p.Fragment = c.Get("HX-Request") == "true"
	p.Input = string(input)
	p.InputFormat = conversion.Format(defaultFormat(from, "json"))
	p.OutputFormat = conversion.Format(defaultFormat(to, "yaml"))
	p.CSVDelimiter = options.CSVDelimiter
	p.InferTypes = options.CSVInferTypes
	p.SpreadsheetSafe = options.SpreadsheetSafe
	p.ParseJSON = options.CSVParseJSON
	p.ColumnRules = c.FormValue("column_types")
	if rules, ok := c.Locals("column_rules").(string); ok {
		p.ColumnRules = rules
	}
	p.RoundTrip = c.FormValue("roundtrip") == "on"
	if checked, ok := c.Locals("roundtrip").(bool); ok {
		p.RoundTrip = checked
	}
	failure := func(message string) error {
		if c.Get("HX-Request") == "true" {
			return fiber.NewError(422, message)
		}
		p.Message = message
		c.Status(422)
		return c.Render("pages/converter", p, "layouts/base")
	}
	columns, rulesErr := conversion.ParseColumnTypes(p.ColumnRules)
	if rulesErr != nil {
		return failure(rulesErr.Error())
	}
	options.CSVColumnTypes = columns
	v, err := conversion.DecodeWithOptions(input, p.InputFormat, options)
	if err != nil {
		return failure("Parsing error: " + conversion.ExplainError(input, err, p.InputFormat))
	}
	result, err := conversion.EncodeConversion(v, input, p.InputFormat, p.OutputFormat, options)
	if err != nil {
		return failure("Conversion error: " + err.Error())
	}
	p.Output = string(result)
	p.OutputReady = true
	p.ConversionNotes = conversion.Notes(p.InputFormat, p.OutputFormat, options)
	p.ConversionNotes = append(p.ConversionNotes, conversion.ValueNotes(v, p.OutputFormat, options)...)
	p.InputTree = conversion.TreePreview(v)
	p.ConversionSummary = fmt.Sprintf("%d input records · %d input bytes → %d output bytes", len(conversion.Rows(v)), len(input), len(result))
	back, backErr := conversion.DecodeWithOptions(result, p.OutputFormat, options)
	if backErr == nil {
		p.OutputTree = conversion.TreePreview(back)
	}
	if p.RoundTrip {
		if backErr != nil {
			p.Differences = []string{"Output could not be read back: " + backErr.Error()}
		} else {
			p.Differences = conversion.Differences(v, back)
			if len(p.Differences) == 0 {
				p.ConversionNotes = append(p.ConversionNotes, "Round-trip check: normalized values and types match. Source formatting is not compared.")
			}
		}
	} else {
		p.ConversionNotes = append(p.ConversionNotes, "Round-trip validation was not requested.")
	}
	if c.Get("HX-Request") == "true" {
		return c.Render("partials/output", p)
	}
	return c.Render("pages/converter", p, "layouts/base")
}
func (s *Server) explorer(c fiber.Ctx) error {
	items, _, err := s.Service.List("", 1, 100)
	if err != nil {
		return err
	}
	return s.render(c, "explorer", "API explorer", func(p *Page) { p.Collections = items })
}
func (s *Server) settings(c fiber.Ctx) error {
	return s.render(c, "settings", "Settings", func(p *Page) {
		p.Watch = s.Service.Setting("watch_enabled", "true") == "true"
		p.APIOptions = s.APIPolicy.Global()
		p.APIOrigins = strings.Join(p.APIOptions.Origins, "\n")
		p.APISettingsMessage = c.Query("api_saved")
	})
}
func (s *Server) saveSettings(c fiber.Ctx) error {
	value := "false"
	if c.FormValue("watch_enabled") == "on" {
		value = "true"
	}
	if err := s.Service.SetSetting("watch_enabled", value); err != nil {
		return err
	}
	if c.Get("HX-Request") == "true" {
		p := s.base(c, "", "settings")
		p.Message = "Settings saved."
		return c.Render("partials/success", p)
	}
	return c.Redirect().Status(303).To("/settings")
}
