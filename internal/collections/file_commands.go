package collections

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fluxport/internal/conversion"
)

// FileCommand is dashboard-only. REST mutation semantics remain unchanged.
type FileCommand struct {
	Action      string   `json:"action"`
	Generation  string   `json:"generation"`
	IDs         []string `json:"ids,omitempty"`
	Fields      []string `json:"fields,omitempty"`
	Name        string   `json:"name,omitempty"`
	Value       any      `json:"value,omitempty"`
	Text        string   `json:"text,omitempty"`
	Convert     bool     `json:"convert,omitempty"`
	Append      bool     `json:"append,omitempty"`
	Find        string   `json:"find,omitempty"`
	Replacement string   `json:"replacement,omitempty"`
	Query       Query    `json:"query,omitempty"`
	Scope       string   `json:"scope,omitempty"`
}
type FileCommandPreview struct {
	Rows    int        `json:"rows"`
	Cells   int        `json:"cells"`
	Columns []string   `json:"columns"`
	Sample  [][]string `json:"sample"`
	Label   string     `json:"label"`
}

func ParseClipboard(text string) ([][]string, error) {
	if len(text) > 1<<20 {
		return nil, fmt.Errorf("paste exceeds 1 MiB")
	}
	var out [][]string
	row := []string{}
	var value strings.Builder
	quoted, after := false, false
	width, cells := -1, 0
	field := func() { row = append(row, value.String()); value.Reset(); after = false }
	flush := func() error {
		field()
		if width < 0 {
			width = len(row)
		}
		if len(row) != width {
			return fmt.Errorf("paste must be rectangular")
		}
		cells += len(row)
		if len(out) >= 1000 || cells > 5000 {
			return fmt.Errorf("paste supports at most 1000 rows and 5000 cells")
		}
		out = append(out, row)
		row = []string{}
		return nil
	}
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if quoted {
			if ch == '"' {
				if i+1 < len(text) && text[i+1] == '"' {
					value.WriteByte('"')
					i++
				} else {
					quoted = false
					after = true
				}
			} else {
				value.WriteByte(ch)
			}
			continue
		}
		if ch == '"' {
			if value.Len() != 0 || after {
				return nil, fmt.Errorf("unexpected quote in clipboard field")
			}
			quoted = true
			continue
		}
		if ch == '\t' {
			field()
			continue
		}
		if ch == '\n' || ch == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			if ch == '\r' {
				i++
			}
			if len(row) > 0 || value.Len() > 0 || after {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			continue
		}
		if after {
			return nil, fmt.Errorf("unexpected text after a quoted cell")
		}
		value.WriteByte(ch)
	}
	if quoted {
		return nil, fmt.Errorf("unclosed quoted clipboard cell")
	}
	if len(row) > 0 || value.Len() > 0 || after {
		if err := flush(); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("paste contains no rows")
	}
	return out, nil
}
func clipboardValue(text string, convert bool) any {
	if !convert {
		return text
	}
	// Explicit conversion never passes through a browser float.
	value, err := conversion.Decode([]byte(strings.TrimSpace(text)), "json")
	if err == nil {
		switch value.(type) {
		case map[string]any, []any, string:
		default:
			return value
		}
	}
	return text
}
func (s *Service) FileCommand(id string, cmd FileCommand, apply bool) (FileCommandPreview, error) {
	var preview FileCommandPreview
	if cmd.Generation == "" {
		return preview, ErrConflict
	}
	if len(cmd.IDs) > 1000 {
		return preview, fmt.Errorf("select at most 1000 rows")
	}
	if len(cmd.Fields) > 2048 {
		return preview, fmt.Errorf("too many columns")
	}
	c, err := s.Get(id)
	if err != nil {
		return preview, err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	c, err = s.ready(c, true)
	if err != nil {
		return preview, err
	}
	if c.Generation != cmd.Generation {
		return preview, ErrConflict
	}
	if c.ReadOnly || c.Size > s.MaxBytes || c.Rows > 100000 {
		return preview, fmt.Errorf("this file is read-only; create a smaller editable copy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	before, err := s.read(c.Path)
	if err != nil {
		return preview, err
	}
	if fileDigest(before) != c.SourceSHA {
		return preview, ErrConflict
	}
	original, err := conversion.Decode(before, c.Format)
	if err != nil {
		return preview, err
	}
	rows := conversion.Rows(original)
	ids, err := s.fileRecordIDs(c)
	if err != nil {
		return preview, err
	}
	if len(ids) != len(rows) {
		return preview, ErrConflict
	}
	beforeIDs := append([]string(nil), ids...)
	positions := map[string]int{}
	keys := map[string]bool{}
	for i, row := range rows {
		positions[ids[i]] = i
		if obj, ok := row.(map[string]any); ok {
			for key := range obj {
				keys[key] = true
			}
		}
	}
	for _, f := range mustFields(c) {
		keys[f.Name] = true
	}
	selected := []int{}
	seen := map[string]bool{}
	for _, id := range cmd.IDs {
		n, ok := positions[id]
		if !ok {
			return preview, ErrConflict
		}
		if seen[id] {
			return preview, fmt.Errorf("row selected twice")
		}
		seen[id] = true
		selected = append(selected, n)
	}
	names := map[string]bool{}
	for _, name := range cmd.Fields {
		if name == "" || len(name) > 4096 || names[name] {
			return preview, fmt.Errorf("column names must be nonempty and unique")
		}
		names[name] = true
	}
	object := func(i int) (map[string]any, error) {
		obj, ok := rows[i].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("this action requires object records")
		}
		return obj, nil
	}
	touched := []int{}
	cells := 0
	label := ""
	var deletedSamples []any
	switch cmd.Action {
	case "paste":
		grid, e := ParseClipboard(cmd.Text)
		if e != nil {
			return preview, e
		}
		if len(cmd.Fields) != len(grid[0]) {
			return preview, fmt.Errorf("paste width does not match destination columns")
		}
		for _, name := range cmd.Fields {
			if !keys[name] && !(cmd.Append && len(rows) == 0) {
				return preview, fmt.Errorf("column %q does not exist; add it first", name)
			}
		}
		if cmd.Append {
			if len(cmd.IDs) > 0 {
				return preview, fmt.Errorf("append cannot overwrite selected rows")
			}
			if _, ok := original.([]any); !ok {
				return preview, fmt.Errorf("only array files support appending")
			}
			for range grid {
				selected = append(selected, len(rows))
				rows = append(rows, map[string]any{})
				ids = append(ids, newRecordID())
			}
		} else if len(selected) != len(grid) {
			return preview, fmt.Errorf("paste requires exactly %d destination rows; no extra rows are overwritten", len(grid))
		}
		for i, row := range grid {
			obj, e := object(selected[i])
			if e != nil {
				return preview, e
			}
			for j, v := range row {
				obj[cmd.Fields[j]] = clipboardValue(v, cmd.Convert)
			}
			touched = append(touched, selected[i])
			cells += len(row)
		}
		label = fmt.Sprintf("Pasted %d cells", cells)
	case "fill_down":
		if len(selected) < 2 || len(cmd.Fields) == 0 || len(selected)*len(cmd.Fields) > 5000 {
			return preview, fmt.Errorf("select 2–1000 rows and at most 5000 cells")
		}
		first, e := object(selected[0])
		if e != nil {
			return preview, e
		}
		for _, i := range selected[1:] {
			obj, e := object(i)
			if e != nil {
				return preview, e
			}
			for _, name := range cmd.Fields {
				if !keys[name] {
					return preview, fmt.Errorf("unknown column %q", name)
				}
				if value, ok := first[name]; ok {
					obj[name] = value
				} else {
					delete(obj, name)
				}
				cells++
			}
			touched = append(touched, i)
		}
		label = fmt.Sprintf("Filled %d cells", cells)
	case "add_row":
		if _, ok := original.([]any); !ok {
			return preview, fmt.Errorf("single documents cannot add rows")
		}
		value := cmd.Value
		if c.Format == "csv" {
			obj, ok := value.(map[string]any)
			if !ok {
				return preview, fmt.Errorf("CSV rows must be objects")
			}
			for name := range keys {
				if _, ok := obj[name]; !ok {
					obj[name] = ""
				}
			}
		}
		rows = append(rows, value)
		ids = append(ids, newRecordID())
		touched = []int{len(rows) - 1}
		label = "Added row"
	case "add_column", "rename_column", "delete_columns":
		if len(rows) == 0 {
			return preview, fmt.Errorf("add a row before changing columns")
		}
		if cmd.Action != "delete_columns" && (cmd.Name == "" || len(cmd.Name) > 4096) {
			return preview, fmt.Errorf("enter a column name")
		}
		if cmd.Action != "delete_columns" && keys[cmd.Name] {
			return preview, fmt.Errorf("column %q already exists", cmd.Name)
		}
		if cmd.Action == "rename_column" && len(cmd.Fields) != 1 {
			return preview, fmt.Errorf("choose one column to rename")
		}
		if cmd.Action != "add_column" && len(cmd.Fields) == 0 {
			return preview, fmt.Errorf("choose columns")
		}
		for _, name := range cmd.Fields {
			if !keys[name] {
				return preview, fmt.Errorf("unknown column %q", name)
			}
		}
		if c.Format == "csv" && cmd.Action == "delete_columns" && len(cmd.Fields) >= len(keys) {
			return preview, fmt.Errorf("CSV must retain at least one column")
		}
		for i := range rows {
			obj, e := object(i)
			if e != nil {
				return preview, e
			}
			switch cmd.Action {
			case "add_column":
				obj[cmd.Name] = cmd.Value
			case "rename_column":
				if value, ok := obj[cmd.Fields[0]]; ok {
					obj[cmd.Name] = value
					delete(obj, cmd.Fields[0])
				}
			case "delete_columns":
				for _, name := range cmd.Fields {
					delete(obj, name)
				}
			}
			touched = append(touched, i)
			cells++
		}
		label = map[string]string{"add_column": "Added column " + cmd.Name, "rename_column": "Renamed column to " + cmd.Name, "delete_columns": "Deleted columns"}[cmd.Action]
	case "delete_rows":
		if len(selected) == 0 {
			return preview, fmt.Errorf("select rows to delete")
		}
		removed := map[int]bool{}
		for _, i := range selected {
			removed[i] = true
			deletedSamples = append(deletedSamples, rows[i])
		}
		kept := []any{}
		keptIDs := []string{}
		for i, row := range rows {
			if !removed[i] {
				kept = append(kept, row)
				keptIDs = append(keptIDs, ids[i])
			}
		}
		rows, ids = kept, keptIDs
		label = fmt.Sprintf("Deleted %d rows", len(selected))
		preview.Rows = len(selected)
	case "find_replace":
		if cmd.Find == "" {
			return preview, fmt.Errorf("enter text to find")
		}
		if len(cmd.Fields) == 0 {
			return preview, fmt.Errorf("choose text columns")
		}
		if cmd.Scope != "selected" {
			ref, e := s.sourceReference(c, c.Generation)
			if e != nil {
				return preview, e
			}
			defer ref.release()
			matches, e := recordsForFileQuery(ctx, ref, cmd.Query)
			if e != nil {
				return preview, e
			}
			selected = nil
			for _, record := range matches {
				selected = append(selected, positions[record.ID])
			}
		} else if len(selected) == 0 {
			return preview, fmt.Errorf("select rows first")
		}
		var replacementBytes int64
		for _, i := range selected {
			obj, e := object(i)
			if e != nil {
				return preview, e
			}
			changed := false
			for _, name := range cmd.Fields {
				if !keys[name] {
					return preview, fmt.Errorf("unknown column %q", name)
				}
				if value, ok := obj[name].(string); ok && strings.Contains(value, cmd.Find) {
					size := int64(len(value)) + int64(strings.Count(value, cmd.Find))*int64(len(cmd.Replacement)-len(cmd.Find))
					replacementBytes += size
					if replacementBytes > s.MaxBytes {
						return preview, ErrTooLarge
					}
					if size > s.RecordBytes {
						return preview, fmt.Errorf("replacement exceeds record limit")
					}
					next := strings.ReplaceAll(value, cmd.Find, cmd.Replacement)
					if int64(len(next)) > s.RecordBytes {
						return preview, fmt.Errorf("replacement exceeds record limit")
					}
					obj[name] = next
					cells++
					changed = true
				}
			}
			if changed {
				touched = append(touched, i)
			}
		}
		label = fmt.Sprintf("Replaced text in %d cells", cells)
	default:
		return preview, fmt.Errorf("unsupported file action")
	}
	if len(rows) > 100000 {
		return preview, fmt.Errorf("file editing supports at most 100000 rows")
	}
	if len(Schema(rows)) > 2048 {
		return preview, fmt.Errorf("file exceeds 2048 columns")
	}
	// Check aggregate output before a structural edit can expand a shared value
	// across thousands of rows. Never allocate an unbounded whole-file encoding.
	var estimated int64
	for _, row := range rows {
		payload, e := conversion.MarshalJSON(row)
		if e != nil {
			return preview, e
		}
		if int64(len(payload)) > s.RecordBytes {
			return preview, fmt.Errorf("record exceeds size limit")
		}
		size := int64(len(payload))
		if c.Format == "yaml" {
			formatted, e := conversion.Encode(row, "yaml")
			if e != nil {
				return preview, e
			}
			size = int64(len(formatted) + 2*bytes.Count(formatted, []byte("\n")))
		}
		estimated += size
		if estimated > s.MaxBytes {
			return preview, ErrTooLarge
		}
	}
	after, err := encodeFileRows(c, original, rows)
	if err != nil {
		return preview, err
	}
	if err = s.validateFileOutput(c, after, len(rows)); err != nil {
		return preview, err
	}
	// Build an exact, bounded sample. Numbers stay serialized strings in previews.
	sampleRows := rows
	if cmd.Action == "add_row" {
		sampleRows = []any{rows[len(rows)-1]}
		touched = []int{0}
	}
	if cmd.Action == "delete_rows" {
		sampleRows = deletedSamples
		for i := range deletedSamples {
			touched = append(touched, i)
		}
	}
	fields := Schema(sampleRows)
	scalarPreview := len(fields) == 0
	if scalarPreview {
		preview.Columns = []string{"Value"}
	}
	for _, f := range fields {
		preview.Columns = append(preview.Columns, f.Name)
	}
	for _, i := range touched {
		if len(preview.Sample) >= 20 {
			break
		}
		sample := []string{}
		for _, name := range preview.Columns {
			obj, ok := sampleRows[i].(map[string]any)
			var v any
			exists := false
			if ok {
				v, exists = obj[name]
			}
			if scalarPreview {
				v, exists = sampleRows[i], true
			}
			if !exists {
				sample = append(sample, "(missing)")
			} else if text, ok := v.(string); ok {
				sample = append(sample, text)
			} else {
				b, e := conversion.MarshalJSON(v)
				if e != nil {
					return preview, e
				}
				sample = append(sample, string(b))
			}
		}
		preview.Sample = append(preview.Sample, sample)
	}
	if cmd.Action != "delete_rows" {
		preview.Rows = len(touched)
	}
	preview.Cells = cells
	preview.Label = label
	if apply && (!bytes.Equal(before, after) || cmd.Action == "add_row") {
		if err = s.publishFileRevision(ctx, c, before, after, beforeIDs, ids, label); err != nil {
			return preview, err
		}
	}
	return preview, nil
}
func mustFields(c Collection) []Field {
	// Schema metadata is trusted only for empty CSV/header preservation.
	var fields []Field
	_ = json.Unmarshal([]byte(c.SchemaJSON), &fields)
	return fields
}
