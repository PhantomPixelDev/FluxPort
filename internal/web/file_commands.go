package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

func (s *Server) editableCopyStatus(c fiber.Ctx) error {
	job, err := s.Service.Jobs.Get(c.Params("job"))
	if err != nil || job.Kind != "editable_copy" || job.Resource != c.Params("id") {
		return fiber.NewError(404, "Editable copy job not found")
	}
	result := ""
	if job.State == "ready" {
		if copy, e := s.Service.Get(job.Result); e == nil {
			result = copy.ID
		}
	}
	return c.JSON(fiber.Map{"id": job.ID, "state": job.State, "records": job.Records, "error": job.Error, "result": result})
}
func (s *Server) fileCommand(c fiber.Ctx) error {
	if err := rejectResultMutation(c); err != nil {
		return err
	}
	var cmd collections.FileCommand
	raw := c.FormValue("command")
	if raw != "" {
		if len(raw) > 1<<20 {
			return fiber.NewError(413, "Edit exceeds request limit")
		}
		if _, err := conversion.Decode([]byte(raw), "json"); err != nil {
			return fiber.NewError(400, err.Error())
		}
		d := json.NewDecoder(bytes.NewBufferString(raw))
		d.UseNumber()
		d.DisallowUnknownFields()
		if err := d.Decode(&cmd); err != nil {
			return fiber.NewError(400, err.Error())
		}
	} else {
		cmd.Action = c.FormValue("command_action")
		if cmd.Action == "add_row" {
			cmd.Value = map[string]any{}
		}
		if cmd.Action == "add_column" {
			cmd.Value = ""
		}
		cmd.Generation = c.FormValue("generation")
		cmd.Name = c.FormValue("column_name")
		cmd.Text = c.FormValue("paste_text")
		cmd.Convert = c.FormValue("convert") == "on"
		cmd.Append = c.FormValue("append") == "on"
		cmd.Find = c.FormValue("find")
		cmd.Replacement = c.FormValue("replacement")
		cmd.Scope = c.FormValue("scope")
		for _, v := range c.Request().PostArgs().PeekMulti("record_id") {
			cmd.IDs = append(cmd.IDs, string(v))
		}
		for _, v := range c.Request().PostArgs().PeekMulti("field") {
			cmd.Fields = append(cmd.Fields, string(v))
		}
		if c.FormValue("new_columns") != "" {
			cmd.Fields = strings.Split(strings.ReplaceAll(c.FormValue("new_columns"), "\r\n", "\n"), "\n")
		}
		var err error
		cmd.Query, err = decodeQuery(c.FormValue("query"))
		if err != nil {
			return fiber.NewError(400, err.Error())
		}
		if value := c.FormValue("command_value"); value != "" {
			cmd.Value, err = conversion.Decode([]byte(value), "json")
			if err != nil {
				return fiber.NewError(400, err.Error())
			}
		}
		data, err := json.Marshal(cmd)
		if err != nil {
			return err
		}
		raw = string(data)
	}
	previewOnly := c.FormValue("preview") == "1"
	encoded := url.Values{"command": {raw}, "csrf": {c.FormValue("csrf")}, "confirm": {"delete"}}
	if len(encoded.Encode()) > 1<<20 {
		return fiber.NewError(413, "Edit exceeds the 1 MiB request limit; use a smaller block")
	}
	if !previewOnly && strings.HasPrefix(cmd.Action, "delete_") && c.FormValue("confirm") != "delete" {
		return fiber.NewError(400, "Confirm deletion before applying")
	}
	preview, err := s.Service.FileCommand(c.Params("id"), cmd, !previewOnly)
	if err != nil {
		if c.Get("Accept") == "application/json" {
			status := 422
			if errors.Is(err, collections.ErrConflict) {
				status = 409
			}
			if errors.Is(err, collections.ErrPending) {
				status = 503
				c.Set("Retry-After", "2")
			}
			return fiber.NewError(status, err.Error())
		}
		c.Locals("editor_action", cmd.Action)
		c.Locals("editor_command", raw)
		return s.workbenchFailure(c, err)
	}
	if previewOnly {
		if c.Get("Accept") == "application/json" {
			return c.JSON(fiber.Map{"preview": preview, "command": raw})
		}
		c.Locals("editor_action", cmd.Action)
		c.Locals("editor_command", raw)
		c.Locals("workbench_query", c.FormValue("query"))
		c.Locals("editor_preview", &preview)
		return s.detail(c)
	}
	return s.renderFileEditResponse(c)
}
func (s *Server) editableCopy(c fiber.Ctx) error {
	q, err := decodeQuery(c.FormValue("query"))
	if err != nil {
		return fiber.NewError(400, err.Error())
	}
	if c.FormValue("scope") == "entire" {
		q = collections.Query{}
	}
	job, err := s.Service.EditableCopyJob(c.Params("id"), c.FormValue("generation"), q)
	if err != nil {
		return fiber.NewError(422, err.Error())
	}
	if c.Get("Accept") == "application/json" {
		return c.Status(202).JSON(job)
	}
	return c.Redirect().Status(303).To(fmt.Sprintf("/collections/%s?copy_job=%s", c.Params("id"), job.ID))
}
