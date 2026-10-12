package conversion

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// CSVFields retains header-only tables without inventing a data row.
func CSVFields(r io.Reader, o Options, limit int64) ([]string, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	if b, _ := br.Peek(3); bytes.Equal(b, utf8BOM) {
		br.Discard(3)
	}
	sample, _ := br.Peek(64 << 10)
	comma, e := csvDelimiter(sample, o.CSVDelimiter)
	if e != nil {
		return nil, e
	}
	b, e := csvRecord(br, limit)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("CSV contains invalid UTF-8")
	}
	cr := csv.NewReader(bytes.NewReader(b))
	cr.Comma = comma
	fields, e := cr.Read()
	if e != nil {
		return nil, e
	}
	return fields, validateCSVFields(fields)
}

func EncodeConversion(v any, input []byte, from, to string, o Options) ([]byte, error) {
	if Format(from) == "csv" && Format(to) == "csv" && len(Rows(v)) == 0 {
		fields, e := CSVFields(bytes.NewReader(input), o, int64(len(input))+1)
		if e != nil {
			return nil, e
		}
		var b bytes.Buffer
		w, e := NewRowWriterWithOptions(&b, "csv", fields, o)
		if e != nil {
			return nil, e
		}
		if e = w.Close(); e != nil {
			return nil, e
		}
		return b.Bytes(), nil
	}
	return EncodeWithOptions(v, to, o)
}

// Options are explicit: CSV has no intrinsic scalar types or lossless nesting.
type Options struct {
	ParquetTyped    bool              `json:"parquet_typed,omitempty"`
	CSVInferTypes   bool              `json:"csv_infer_types,omitempty"`
	CSVDelimiter    string            `json:"csv_delimiter,omitempty"`
	SpreadsheetSafe bool              `json:"spreadsheet_safe,omitempty"`
	CSVColumnTypes  map[string]string `json:"csv_column_types,omitempty"`
	CSVParseJSON    bool              `json:"csv_parse_json,omitempty"`
}

func DecodeWithOptions(b []byte, f string, o Options) (any, error) {
	if Format(f) != "csv" {
		return Decode(b, f)
	}
	b = bytes.TrimPrefix(b, utf8BOM)
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errEmptyInput
	}
	if !utf8.Valid(b) {
		return nil, fmt.Errorf("CSV contains invalid UTF-8")
	}
	comma, e := csvDelimiter(b, o.CSVDelimiter)
	if e != nil {
		return nil, e
	}
	r := csv.NewReader(bytes.NewReader(b))
	r.Comma = comma
	header, e := r.Read()
	if e != nil {
		return nil, fmt.Errorf("CSV header: %w", e)
	}
	if e = validateCSVFields(header); e != nil {
		return nil, e
	}
	if e = validateColumnRules(header, o); e != nil {
		return nil, e
	}
	rows := []any{}
	for {
		cells, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		row := map[string]any{}
		for i, k := range header {
			value, err := csvColumn(cells[i], k, o)
			if err != nil {
				return nil, fmt.Errorf("CSV row %d, column %q: %w", len(rows)+2, k, err)
			}
			row[k] = value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func csvDelimiter(sample []byte, choice string) (rune, error) {
	switch choice {
	case "", "comma", ",":
		return ',', nil
	case "semicolon", ";":
		return ';', nil
	case "tab", "\t":
		return '\t', nil
	case "pipe", "|":
		return '|', nil
	case "auto":
		best, score := rune(','), 1
		ambiguous := false
		for _, comma := range []rune{',', ';', '\t', '|'} {
			r := csv.NewReader(bytes.NewReader(sample))
			r.Comma = comma
			header, e := r.Read()
			if e != nil || validateCSVFields(header) != nil {
				continue
			}
			n := len(header)
			valid := true
			for i := 0; i < 7; i++ {
				_, e = r.Read()
				if e == io.EOF {
					break
				}
				if e != nil { // Ignore a truncated final sample record after a valid header.
					if r.InputOffset() >= int64(len(sample)) {
						break
					}
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			if n > score {
				best, score, ambiguous = comma, n, false
			} else if n == score && n > 1 {
				ambiguous = true
			}
		}
		if ambiguous {
			return 0, fmt.Errorf("CSV delimiter is ambiguous; select comma, semicolon, tab or pipe")
		}
		return best, nil
	default:
		return 0, fmt.Errorf("unsupported CSV delimiter %q", choice)
	}
}
func validateCSVFields(fields []string) error {
	if len(fields) == 0 {
		return fmt.Errorf("CSV requires at least one field")
	}
	seen := map[string]bool{}
	for _, k := range fields {
		if k == "" || seen[k] {
			return fmt.Errorf("CSV headers must be non-empty and unique")
		}
		seen[k] = true
	}
	return nil
}
func csvScalar(s string, infer bool) any {
	if !infer {
		return s
	}
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	// JSON's number grammar deliberately excludes leading-zero IDs, whitespace,
	// plus prefixes, NaN/Infinity and locale-specific decimal/grouping separators.
	if s != "" && strings.TrimSpace(s) == s && json.Valid([]byte(s)) {
		if s[0] == '-' || (s[0] >= '0' && s[0] <= '9') {
			return json.Number(s)
		}
	}
	return s
}
func spreadsheetCell(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n\ufeff")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + s
	}
	if len(s) > 0 && (s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		return "'" + s
	}
	return s
}
func EncodeWithOptions(v any, f string, o Options) ([]byte, error) {
	if Format(f) == "csv" {
		return encodeCSV(v, o.SpreadsheetSafe)
	}
	return Encode(v, f)
}
func encodeCSV(v any, safe bool) ([]byte, error) {
	rows := Rows(v)
	keys := map[string]bool{}
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("CSV requires objects or an array of objects")
		}
		for k := range obj {
			keys[k] = true
		}
	}
	fields := make([]string, 0, len(keys))
	for k := range keys {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	if e := validateCSVFields(fields); e != nil {
		return nil, e
	}
	header := append([]string(nil), fields...)
	if safe {
		for i := range header {
			header[i] = spreadsheetCell(header[i])
		}
		if e := validateCSVFields(header); e != nil {
			return nil, fmt.Errorf("spreadsheet-safe headers collide: %w", e)
		}
	}
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	if e := w.Write(header); e != nil {
		return nil, e
	}
	for _, row := range rows {
		obj := row.(map[string]any)
		cells := make([]string, len(fields))
		for i, k := range fields {
			value := obj[k]
			if value == nil {
				continue
			}
			if s, ok := value.(string); ok {
				if safe {
					s = spreadsheetCell(s)
				}
				cells[i] = s
			} else {
				b, e := marshalJSON(value)
				if e != nil {
					return nil, e
				}
				cells[i] = string(b)
			}
		}
		if e := w.Write(cells); e != nil {
			return nil, e
		}
	}
	w.Flush()
	return out.Bytes(), w.Error()
}

func Notes(from, to string, o Options) []string {
	notes := []string{}
	if Format(from) == "csv" {
		if o.CSVInferTypes {
			notes = append(notes, "CSV inference converts JSON-style numbers, true/false and null. Leading-zero IDs, empty cells and other text stay strings.")
		} else {
			notes = append(notes, "CSV input cells remain strings; enable type inference for numbers, booleans and null.")
		}
	}
	switch Format(to) {
	case "csv":
		notes = append(notes, "CSV is a flat text format: nested objects/arrays become JSON text inside cells; missing values and null become empty cells.")
		if o.SpreadsheetSafe {
			notes = append(notes, "Spreadsheet-safe CSV prefixes formula-like text and headers with an apostrophe. Numeric values remain numeric text; disable this option for exact text-cell preservation.")
		}
	case "xml":
		notes = append(notes, "XML output uses a FluxPort JSON envelope to preserve nested arrays, scalar types and nulls. XML entities such as &lt; and &amp; are required escaping.")
	case "ndjson":
		notes = append(notes, "NDJSON writes one JSON value per line; an array root becomes a sequence of records.")
	case "toml":
		notes = append(notes, "TOML requires an object root, has no null and supports signed 64-bit integers. Conversions that lose numeric precision are rejected.")
	}
	if Format(from) == "xml" {
		notes = append(notes, "XML attributes use @keys. Mixed text and child elements use an ordered #content list so text position is retained.")
	}
	if Format(from) == "toml" {
		notes = append(notes, "TOML date/time types become ISO strings; timezone and datetime spelling may be normalized.")
	}
	return notes
}
