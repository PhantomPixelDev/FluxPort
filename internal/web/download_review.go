package web

import (
	"context"
	"errors"
	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"fluxport/internal/quality"
	"fmt"
	"github.com/gofiber/fiber/v3"
	"net/url"
	"time"
)

type DownloadReview struct {
	Format, Pipeline string
	Notes            []string
	Details          []string
	Checked          int
	Total            int64
	Saved, Blocked   bool
}

func (s *Server) reviewDownload(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	format := c.FormValue("format", c.Query("format"))
	valid := format == "original"
	for _, f := range collections.ExportFormats() {
		if f == format {
			valid = true
		}
	}
	if !valid {
		return fiber.NewError(422, "Choose a converted output format to review")
	}
	p := s.base(c, "Check download format", "collections")
	p.Collection = col
	review := &DownloadReview{Format: format, Pipeline: c.FormValue("pipeline", c.Query("pipeline")), Total: col.Rows}
	p.DownloadReview = review
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/collections/"+col.ID+"/export-check?format="+url.QueryEscape(format)+"&pipeline="+url.QueryEscape(review.Pipeline))
		return c.SendStatus(200)
	}
	if format == "original" {
		review.Notes = []string{"Exact source bytes, with no conversion. Excel downloads include the entire original workbook."}
		return c.Render("pages/download-review", p, "layouts/base")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	rows := []any{}
	if review.Pipeline != "" {
		recipe, e := s.Service.GetPipeline(review.Pipeline)
		if e != nil || recipe.Source != col.ID {
			return fiber.NewError(422, "Saved steps not found for this dataset")
		}
		rows, err = s.Service.PreviewPipeline(ctx, recipe, -1)
		review.Saved = true
	} else {
		stop := errors.New("sample complete")
		bytes := 0
		err = s.Service.EachQuery(ctx, col, collections.Query{}, col.Generation, func(row any) error {
			raw, e := conversion.MarshalJSON(row)
			if e != nil {
				return e
			}
			if len(rows) >= 100 || bytes+len(raw) > 8<<20 {
				return stop
			}
			bytes += len(raw)
			rows = append(rows, row)
			return nil
		})
		if errors.Is(err, stop) {
			err = nil
		}
	}
	if err != nil {
		if errors.Is(err, collections.ErrPending) {
			c.Set("Retry-After", "2")
			return fiber.NewError(503, "Dataset is processing. Try the format check again when it is ready.")
		}
		return fiber.NewError(422, err.Error())
	}
	review.Checked = len(rows)
	options := conversion.Options{SpreadsheetSafe: review.Saved}
	review.Notes = append(review.Notes, conversion.Notes(col.Format, format, options)...)
	review.Notes = append(review.Notes, conversion.ValueNotes(rows, format, options)...)
	if note := collections.BinaryNotes(format); note != "" {
		review.Notes = append(review.Notes, note)
	}
	profiler := quality.NewProfilerWithLimit(2048)
	scalar := false
	for _, row := range rows {
		if _, ok := row.(map[string]any); !ok {
			scalar = true
		}
		if err = profiler.Add(row); err != nil {
			return err
		}
	}
	for _, column := range profiler.Columns() {
		if format == "xlsx" || format == "csv" || format == "parquet-typed" {
			if column.Missing+column.Null+column.Empty > 0 {
				review.Details = append(review.Details, fmt.Sprintf("%s: %d missing, %d null and %d empty text values in checked rows. %s", column.Name, column.Missing, column.Null, column.Empty, map[string]string{"xlsx": "These become blank cells.", "csv": "These become empty text cells.", "parquet-typed": "Missing and null become null; empty text stays text."}[format]))
			}
			for _, kind := range column.Types {
				if kind == "object" || kind == "array" {
					review.Details = append(review.Details, column.Name+": nested values become JSON text.")
					break
				}
			}
		}
		if format == "xlsx" && column.NumericCount > 0 {
			review.Details = append(review.Details, column.Name+": numeric values become exact text cells, not Excel numbers.")
		}
	}
	if scalar && (format == "xlsx" || format == "parquet-typed") {
		review.Blocked = true
		review.Notes = append(review.Notes, "This format requires object rows. Choose lossless Parquet, JSON or NDJSON for scalar rows.")
	}
	if format == "parquet-typed" {
		types, e := collections.TypedParquetPreview(rows)
		if e != nil {
			review.Blocked = true
			review.Notes = append(review.Notes, e.Error())
		} else {
			review.Details = append(review.Details, types...)
		}
	}
	if conversion.Supported(format) {
		if _, e := conversion.Encode(rows, format); e != nil {
			review.Blocked = true
			review.Notes = append(review.Notes, "The checked rows cannot be converted: "+e.Error())
		}
	}
	return c.Render("pages/download-review", p, "layouts/base")
}
