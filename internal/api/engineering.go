package api

import (
	"fluxport/internal/collections"
	"fluxport/internal/quality"
	"github.com/gofiber/fiber/v3"
	"strings"
)

func (a *API) compareDatasets(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	var request collections.CompareRequest
	if err = decodeRequest(c, &request); err != nil {
		return err
	}
	other, err := a.Service.Get(request.Other)
	if err != nil || !other.Enabled {
		return fiber.NewError(404, "Comparison collection not found.")
	}
	if other.Rules != "" && other.Rules != "{}" || col.Rules != "" && col.Rules != "{}" {
		return fiber.NewError(422, "Comparison requires endpoints without response transformations.")
	}
	report, err := a.Service.Compare(c.Context(), col.ID, request)
	if err != nil {
		return indexedError(c, err)
	}
	return c.JSON(report)
}
func (a *API) contract(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	if c.Method() == "PUT" {
		var contract quality.Contract
		if err = decodeRequest(c, &contract); err != nil {
			return err
		}
		if err = a.Service.SaveContract(col.ID, &contract); err != nil {
			return fiber.NewError(422, err.Error())
		}
		return c.SendStatus(204)
	}
	if c.Method() == "DELETE" {
		if err = a.Service.SaveContract(col.ID, nil); err != nil {
			return err
		}
		return c.SendStatus(204)
	}
	contract, err := a.Service.Contract(col.ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"contract": contract})
}
func (a *API) qualityExport(c fiber.Ctx) error {
	col, err := a.collection(c)
	if err != nil {
		return err
	}
	if rules := strings.TrimSpace(col.Rules); rules != "" && rules != "{}" {
		return fiber.NewError(422, "Checked exports require an endpoint without response transformations.")
	}
	var request struct {
		Mode string `json:"mode"`
	}
	if err = decodeRequest(c, &request); err != nil {
		return err
	}
	j, err := a.Service.QualityExportJob(col.ID, request.Mode)
	if err != nil {
		return indexedError(c, err)
	}
	return c.Status(202).JSON(j)
}

func (a *API) auditPipeline(c fiber.Ctx) error {
	p, err := a.Service.GetPipeline(c.Params("pipeline"))
	if err != nil {
		return fiber.NewError(404, "Recipe not found.")
	}
	if err = a.pipelineAccess(p); err != nil {
		return err
	}
	j, err := a.Service.PipelineAuditJob(p)
	if err != nil {
		return indexedError(c, err)
	}
	return c.Status(202).JSON(j)
}

func engineeringPaths(paths map[string]any, base string) {
	body := func(properties fiber.Map, required ...string) fiber.Map {
		return fiber.Map{"required": true, "content": fiber.Map{"application/json": fiber.Map{"schema": fiber.Map{"type": "object", "properties": properties, "required": required}}}}
	}
	op := func(summary string, code string) fiber.Map {
		return fiber.Map{"summary": summary, "responses": fiber.Map{code: fiber.Map{"description": summary}, "422": fiber.Map{"description": "Invalid input or resource limit exceeded"}}}
	}
	compare := op("Compare complete datasets by unique literal keys; return exact counts and up to 20 differences", "200")
	compare["requestBody"] = body(fiber.Map{"other": fiber.Map{"type": "string"}, "keys": fiber.Map{"type": "array", "minItems": 1, "maxItems": 8, "items": fiber.Map{"type": "string"}}}, "other", "keys")
	paths[base+"/compare"] = fiber.Map{"post": compare}
	contract := fiber.Map{"columns": fiber.Map{"type": "array", "minItems": 1, "maxItems": 256, "items": fiber.Map{"type": "object"}}, "allow_extra": fiber.Map{"type": "boolean"}}
	put := op("Save a schema contract using literal top-level column names", "204")
	put["requestBody"] = body(contract, "columns")
	paths[base+"/contract"] = fiber.Map{"get": op("Read the saved schema contract", "200"), "put": put, "delete": op("Remove the saved schema contract", "204")}
	export := op("Prepare a cancellable checked-record ZIP download", "202")
	export["requestBody"] = body(fiber.Map{"mode": fiber.Map{"type": "string", "enum": []string{"stop", "separate"}}}, "mode")
	paths[base+"/validation-export"] = fiber.Map{"post": export}
}
