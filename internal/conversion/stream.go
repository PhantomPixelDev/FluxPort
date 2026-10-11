package conversion

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func Streaming(format string) bool {
	switch Format(format) {
	case "json", "csv", "ndjson":
		return true
	}
	return false
}

// Iterate holds at most one size-bounded record. JSON documents remain bounded;
// top-level arrays, CSV and NDJSON have no whole-source limit.
func Iterate(ctx context.Context, r io.Reader, format string, recordLimit, documentLimit int64, emit func(any) error) error {
	return IterateWithOptions(ctx, r, format, recordLimit, documentLimit, Options{}, emit)
}
func IterateWithOptions(ctx context.Context, r io.Reader, format string, recordLimit, documentLimit int64, options Options, emit func(any) error) error {
	if recordLimit < 1 || documentLimit < 1 {
		return fmt.Errorf("invalid parser limits")
	}
	br := bufio.NewReaderSize(contextReader{ctx: ctx, r: r}, 64<<10)
	if prefix, _ := br.Peek(3); bytes.Equal(prefix, utf8BOM) {
		br.Discard(3)
	}
	check := func() error { return ctx.Err() }
	switch Format(format) {
	case "ndjson":
		line := 0
		for {
			if err := check(); err != nil {
				return err
			}
			b, err := boundedLine(br, recordLimit)
			if err == io.EOF && len(b) == 0 {
				return nil
			}
			if err != nil && err != io.EOF {
				return err
			}
			line++
			if len(bytes.TrimSpace(b)) > 0 {
				v, e := Decode(b, "json")
				if e != nil {
					return fmt.Errorf("line %d: %w", line, e)
				}
				if e = emit(v); e != nil {
					return e
				}
			}
			if err == io.EOF {
				return nil
			}
		}
	case "csv":
		sample, _ := br.Peek(64 << 10)
		comma, e := csvDelimiter(sample, options.CSVDelimiter)
		if e != nil {
			return e
		}
		var header []string
		line := 0
		for {
			if err := check(); err != nil {
				return err
			}
			b, err := csvRecord(br, recordLimit)
			if err == io.EOF && len(b) == 0 {
				if header == nil {
					return fmt.Errorf("CSV has no header")
				}
				return nil
			}
			if err != nil && err != io.EOF {
				return err
			}
			if !utf8.Valid(b) {
				return fmt.Errorf("CSV contains invalid UTF-8")
			}
			cr := csv.NewReader(bytes.NewReader(b))
			cr.Comma = comma
			cells, e := cr.Read()
			if e == io.EOF {
				if err == io.EOF {
					return nil
				}
				continue
			}
			if e != nil {
				return fmt.Errorf("CSV record %d: %w", line+1, e)
			}
			line++
			if header == nil {
				seen := map[string]bool{}
				for _, h := range cells {
					if h == "" || seen[h] {
						return fmt.Errorf("CSV headers must be non-empty and unique")
					}
					seen[h] = true
				}
				header = cells
				if e = validateColumnRules(header, options); e != nil {
					return e
				}
			} else {
				if len(cells) != len(header) {
					return fmt.Errorf("CSV record %d has %d fields; expected %d", line, len(cells), len(header))
				}
				v := map[string]any{}
				for i, h := range header {
					value, err := csvColumn(cells[i], h, options)
					if err != nil {
						return fmt.Errorf("CSV record %d, column %q: %w", line, h, err)
					}
					v[h] = value
				}
				if e = emit(v); e != nil {
					return e
				}
			}
			if err == io.EOF {
				return nil
			}
		}
	case "json":
		first, err := nextNonSpace(br)
		if err != nil {
			return err
		}
		if first != '[' {
			b, err := io.ReadAll(io.LimitReader(io.MultiReader(bytes.NewReader([]byte{first}), br), documentLimit+1))
			if err != nil {
				return err
			}
			if int64(len(b)) > documentLimit {
				return fmt.Errorf("document exceeds %d byte limit; large JSON must use an array root", documentLimit)
			}
			v, err := Decode(b, "json")
			if err != nil {
				return err
			}
			return emit(v)
		}
		next, err := nextNonSpace(br)
		if err != nil {
			return err
		}
		if next == ']' {
			return trailingJSON(br)
		}
		br.UnreadByte()
		for {
			if err := check(); err != nil {
				return err
			}
			b, delim, err := jsonRecord(br, recordLimit)
			if err != nil {
				return err
			}
			v, err := Decode(b, "json")
			if err != nil {
				return err
			}
			if err = emit(v); err != nil {
				return err
			}
			if delim == ']' {
				return trailingJSON(br)
			}
			next, err = nextNonSpace(br)
			if err != nil {
				return err
			}
			if next == ']' {
				return fmt.Errorf("trailing comma in JSON array")
			}
			br.UnreadByte()
		}
	default:
		b, err := io.ReadAll(io.LimitReader(br, documentLimit+1))
		if err != nil {
			return err
		}
		if int64(len(b)) > documentLimit {
			return fmt.Errorf("%s document exceeds %d byte limit", format, documentLimit)
		}
		v, err := DecodeWithOptions(b, format, options)
		if err != nil {
			return err
		}
		for _, row := range Rows(v) {
			if err = check(); err != nil {
				return err
			}
			if err = emit(row); err != nil {
				return err
			}
		}
		return nil
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func boundedLine(r *bufio.Reader, max int64) ([]byte, error) {
	var out []byte
	for {
		b, err := r.ReadSlice('\n')
		if int64(len(out)+len(b)) > max {
			return nil, fmt.Errorf("record exceeds %d byte limit", max)
		}
		out = append(out, b...)
		if err != bufio.ErrBufferFull {
			return out, err
		}
	}
}
func csvRecord(r *bufio.Reader, max int64) ([]byte, error) {
	var out []byte
	quoted := false
	for {
		b, err := boundedLine(r, max-int64(len(out)))
		if err != nil && err != io.EOF {
			return nil, err
		}
		for _, c := range b {
			if c == '"' {
				quoted = !quoted
			}
		}
		out = append(out, b...)
		if !quoted || err == io.EOF {
			return out, err
		}
	}
}
func nextNonSpace(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b, nil
		}
	}
}
func trailingJSON(r *bufio.Reader) error {
	_, err := nextNonSpace(r)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("data after JSON array")
}
func jsonRecord(r *bufio.Reader, max int64) ([]byte, byte, error) {
	out := make([]byte, 0, 512)
	depth := 0
	quoted, escaped := false, false
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, 0, fmt.Errorf("unfinished JSON array: %w", err)
		}
		if !quoted && depth == 0 && (b == ',' || b == ']') {
			if len(bytes.TrimSpace(out)) == 0 {
				return nil, 0, fmt.Errorf("empty JSON record")
			}
			return out, b, nil
		}
		if int64(len(out)) >= max {
			return nil, 0, fmt.Errorf("record exceeds %d byte limit", max)
		}
		out = append(out, b)
		if quoted {
			if escaped {
				escaped = false
			} else if b == '\\' {
				escaped = true
			} else if b == '"' {
				quoted = false
			}
			continue
		}
		switch b {
		case '"':
			quoted = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth < 0 {
				return nil, 0, fmt.Errorf("unbalanced JSON record")
			}
		}
	}
}

// RowWriter emits a sequence; TOML sequences are explicitly wrapped in records.
// Output matches Encode byte for byte: indent level, escaping rules, sequence
// item layout and empty-stream results are shared with the buffered path.
type RowWriter struct {
	w        *bufio.Writer
	format   string
	fields   []string
	count    int64
	csv      *csv.Writer
	safe     bool
	fieldSet map[string]bool
}

func NewRowWriter(w io.Writer, format string, fields []string) (*RowWriter, error) {
	return NewRowWriterWithOptions(w, format, fields, Options{})
}
func NewRowWriterWithOptions(w io.Writer, format string, fields []string, options Options) (*RowWriter, error) {
	format = Format(format)
	if !Supported(format) {
		return nil, fmt.Errorf("unsupported output format %q", format)
	}
	rw := &RowWriter{w: bufio.NewWriterSize(w, 64<<10), format: format, fields: append([]string(nil), fields...), safe: options.SpreadsheetSafe, fieldSet: map[string]bool{}}
	var err error
	switch format {
	case "json":
		_, err = rw.w.WriteString("[\n")
	case "xml":
		_, err = rw.w.WriteString(xml.Header + "<fluxport encoding=\"json\">[")
	case "csv":
		if len(fields) == 0 {
			return nil, fmt.Errorf("CSV requires object fields")
		}
		rw.csv = csv.NewWriter(rw.w)
		if err = validateCSVFields(fields); err != nil {
			return nil, err
		}
		header := append([]string(nil), fields...)
		for i, k := range fields {
			rw.fieldSet[k] = true
			if rw.safe {
				header[i] = spreadsheetCell(k)
			}
		}
		if err = validateCSVFields(header); err != nil {
			return nil, fmt.Errorf("spreadsheet-safe headers collide: %w", err)
		}
		err = rw.csv.Write(header)
	}
	return rw, err
}
func (w *RowWriter) Write(row any) error {
	var err error
	switch w.format {
	case "json", "ndjson", "xml":
		b, e := encodeRow(row, w.format)
		if e != nil {
			return e
		}
		if w.format == "xml" {
			if w.count > 0 {
				if _, err = w.w.WriteString(","); err != nil {
					return err
				}
			}
			err = escapeXMLText(w.w, b)
		} else {
			if w.format == "json" && w.count > 0 {
				if _, err = w.w.WriteString(",\n"); err != nil {
					return err
				}
			}
			_, err = w.w.Write(b)
			if err == nil && w.format == "ndjson" {
				err = w.w.WriteByte('\n')
			}
		}
	case "yaml":
		b, e := Encode([]any{row}, "yaml")
		if e != nil {
			return e
		}
		_, err = w.w.Write(b)
	case "toml":
		if _, ok := row.(map[string]any); !ok {
			return fmt.Errorf("TOML streaming requires object records")
		}
		if w.count > 0 {
			// go-toml separates array-of-table entries with a blank line, and
			// the per-row encoding above ends without one.
			if _, err = w.w.WriteString("\n"); err != nil {
				return err
			}
		}
		b, e := Encode(map[string]any{"records": []any{row}}, "toml")
		if e != nil {
			return e
		}
		_, err = w.w.Write(b)
	case "csv":
		obj, ok := row.(map[string]any)
		if !ok {
			return fmt.Errorf("CSV requires object records")
		}
		for k := range obj {
			if !w.fieldSet[k] {
				return fmt.Errorf("CSV record contains field %q outside the export schema; refusing to drop data", k)
			}
		}
		cells := make([]string, len(w.fields))
		for i, k := range w.fields {
			if obj[k] == nil {
				continue
			}
			if s, ok := obj[k].(string); ok {
				cells[i] = s
				if w.safe {
					cells[i] = spreadsheetCell(s)
				}
			} else {
				b, e := marshalJSON(obj[k])
				if e != nil {
					return e
				}
				cells[i] = string(b)
			}
		}
		err = w.csv.Write(cells)
	}
	w.count++
	return err
}

// encodeRow serializes one record the way the buffered Encode path would write
// it. JSON rows are indented to match a marshal of the whole sequence, so the
// two paths agree byte for byte; NDJSON and the XML envelope stay compact
// because their buffered counterparts are single-line records.
func encodeRow(row any, format string) ([]byte, error) {
	if format == "json" {
		b, err := MarshalJSONIndent(row)
		if err != nil {
			return nil, err
		}
		return indentLines(b, "  "), nil
	}
	return marshalJSON(row)
}

// indentLines prefixes every line, leaving lines that are already blank alone so
// re-indenting never introduces trailing whitespace.
func indentLines(b []byte, indent string) []byte {
	if len(b) == 0 {
		return b
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = indent + line
	}
	return []byte(strings.Join(lines, "\n"))
}

func (w *RowWriter) Close() error {
	if w.csv != nil {
		w.csv.Flush()
		if err := w.csv.Error(); err != nil {
			return err
		}
	}
	switch w.format {
	case "json":
		if _, err := w.w.WriteString("\n]"); err != nil {
			return err
		}
	case "xml":
		if _, err := w.w.WriteString("]</fluxport>\n"); err != nil {
			return err
		}
	case "yaml":
		if w.count == 0 {
			if _, err := w.w.WriteString("[]\n"); err != nil {
				return err
			}
		}
	case "toml":
		if w.count == 0 {
			if _, err := w.w.WriteString("records = []\n"); err != nil {
				return err
			}
		}
	}
	return w.w.Flush()
}
