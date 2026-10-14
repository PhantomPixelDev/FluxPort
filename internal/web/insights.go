package web

import (
	"context"
	"fluxport/internal/collections"
	"fluxport/internal/quality"
	"github.com/gofiber/fiber/v3"
	"strconv"
	"time"
)

type DatasetInsights struct {
	Fields   []collections.Field
	Report   collections.QualityReport
	Columns  []quality.Column
	Field    string
	Expanded int
}

func (s *Server) datasetInsights(c fiber.Ctx) error {
	col, err := s.Service.Get(c.Params("id"))
	if err != nil {
		return err
	}
	p := s.base(c, "Understand data", "collections")
	p.Collection = col
	ctx, cancel := context.WithTimeout(c.Context(), 25*time.Second)
	defer cancel()
	fields, err := s.Service.Fields(col)
	if err != nil {
		return err
	}
	field := c.Query("field")
	if field == "" && len(fields) > 256 {
		field = fields[0].Name
		p.Message = "This is a wide dataset. Showing one column; choose another below."
	}
	report, err := s.Service.Quality(ctx, col, collections.QualityRequest{Limit: 10000, Column: field})
	if err != nil {
		return err
	}
	insight := &DatasetInsights{Report: report, Field: field, Fields: fields, Expanded: -1}
	for _, column := range report.Columns {
		if insight.Field == "" || column.Name == insight.Field {
			insight.Columns = append(insight.Columns, column)
		}
	}
	if insight.Field != "" && len(insight.Columns) > 0 {
		insight.Expanded = 0
	}
	if value := c.Query("detail"); value != "" {
		if index, err := strconv.Atoi(value); err == nil && index >= 0 && index < len(insight.Columns) {
			insight.Expanded = index
		}
	}
	p.Insights = insight
	return s.renderCollectionView(c, "insights", p)
}

func (s *Server) selectWorkbookSheet(c fiber.Ctx) error {
	if err := s.Service.SelectWorkbookSheet(c.Params("id"), c.FormValue("sheet")); err != nil {
		return fiber.NewError(422, err.Error())
	}
	return c.Redirect().Status(303).To("/collections/" + c.Params("id"))
}
