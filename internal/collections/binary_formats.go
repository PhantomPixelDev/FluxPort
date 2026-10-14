package collections

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fluxport/internal/conversion"
	"fluxport/internal/jobs"
	"fluxport/internal/resources"
	"github.com/xuri/excelize/v2"
)

func binaryFormat(format string) bool { return format == "xlsx" || format == "parquet" }
func datasetSupported(format string) bool {
	return binaryFormat(format) || conversion.Supported(format)
}
func DatasetFormats() []string {
	return append(append([]string{}, conversion.Formats...), "xlsx", "parquet")
}

func (s *Service) iterateBinary(ctx context.Context, path, format string, emit func(any) error) error {
	if err := s.safe(path); err != nil {
		return err
	}
	check := func(row any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := conversion.MarshalJSON(row)
		if err != nil {
			return err
		}
		if int64(len(b)) > s.RecordBytes {
			return fmt.Errorf("normalized record exceeds %d bytes", s.RecordBytes)
		}
		return emit(row)
	}
	if format == "xlsx" {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > s.MaxBytes {
			return ErrTooLarge
		}
		if err = checkWorkbook(ctx, path); err != nil {
			return err
		}
		book, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true, UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 4 << 20, TmpDir: s.IndexDir})
		if err != nil {
			return err
		}
		defer book.Close()
		sheets := book.GetSheetList()
		if len(sheets) == 0 {
			return fmt.Errorf("Excel has no sheets")
		}
		selected := s.Setting("workbook_sheet:"+path, sheets[0])
		found := false
		for _, sheet := range sheets {
			if sheet == selected {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("Selected sheet %q is no longer in the workbook; choose another sheet", selected)
		}
		rows, err := book.Rows(selected)
		if err != nil {
			return err
		}
		defer rows.Close()
		var headers []string
		rowNumber := 0
		for rows.Next() {
			rowNumber++
			if err = ctx.Err(); err != nil {
				return err
			}
			cells, e := rows.Columns(excelize.Options{RawCellValue: true})
			if e != nil {
				return e
			}
			if headers == nil {
				headers = cells
				if len(headers) == 0 || len(headers) > 2048 {
					return fmt.Errorf("Excel requires 1–2048 header columns")
				}
				seen := map[string]bool{}
				for _, h := range headers {
					if h == "" || seen[h] {
						return fmt.Errorf("Excel headers must be non-empty and unique")
					}
					seen[h] = true
				}
				continue
			}
			if len(cells) > len(headers) {
				return fmt.Errorf("Excel row %d exceeds header width", rowNumber)
			}
			obj := map[string]any{}
			for i, h := range headers {
				value := ""
				if i < len(cells) {
					value = cells[i]
				}
				obj[h] = value
			}
			if err = check(obj); err != nil {
				return err
			}
		}
		if headers == nil {
			return fmt.Errorf("Excel has no header")
		}
		return rows.Error()
	}
	return s.withBinaryDuckDB(ctx, func(ctx context.Context, db *sql.DB) error {
		// Paths come exclusively from validated staged sources. User SQL is never
		// passed to this trusted file reader, and extensions cannot auto-install.
		encodedLimit := s.RecordBytes*2 + 1024 // Account for escaped JSON in our envelope.
		rows, err := db.QueryContext(ctx, `WITH source AS (SELECT CAST(to_json(t) AS VARCHAR) AS record_json FROM read_parquet(`+quoteSQL(path)+`) t) SELECT CASE WHEN octet_length(encode(record_json))>`+fmt.Sprint(encodedLimit)+` THEN error('Parquet record exceeds record limit') ELSE record_json END FROM source`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var text string
			if err = rows.Scan(&text); err != nil {
				return err
			}
			if int64(len(text)) > encodedLimit {
				return fmt.Errorf("Parquet record exceeds record limit")
			}
			v, e := conversion.Decode([]byte(text), "json")
			if e != nil {
				return e
			}
			if obj, ok := v.(map[string]any); ok && len(obj) == 1 {
				if payload, ok := obj["__fluxport_record_json_v1"].(string); ok {
					v, e = conversion.Decode([]byte(payload), "json")
					if e != nil {
						return fmt.Errorf("invalid FluxPort Parquet envelope: %w", e)
					}
				}
			}
			if err = check(v); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// Inspect worksheet XML as a stream, without loading every cell to find formulas.
func checkWorkbook(ctx context.Context, path string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	var expanded uint64
	seen := map[string]bool{}
	if len(archive.File) > 4096 {
		return fmt.Errorf("Excel archive exceeds 4096 entries")
	}
	for _, entry := range archive.File {
		if err = ctx.Err(); err != nil {
			return err
		}
		name := entry.Name
		if seen[name] || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || strings.Contains("/"+name, "/../") {
			return fmt.Errorf("Excel archive contains an unsafe or duplicate entry")
		}
		seen[name] = true
		if entry.UncompressedSize64 > 64<<20 || expanded > 64<<20-entry.UncompressedSize64 {
			return fmt.Errorf("Excel archive exceeds 64 MiB expanded size")
		}
		expanded += entry.UncompressedSize64
		if !strings.HasPrefix(name, "xl/worksheets/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		reader, e := entry.Open()
		if e != nil {
			return e
		}
		decoder := xml.NewDecoder(checkedReader{ctx: ctx, r: io.LimitReader(reader, (64<<20)+1)})
		for {
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				reader.Close()
				return fmt.Errorf("invalid worksheet XML: %w", e)
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local == "f" {
				reader.Close()
				return fmt.Errorf("Excel contains formulas; replace formulas with values before import")
			}
		}
		if err = reader.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) withBinaryDuckDB(parent context.Context, run func(context.Context, *sql.DB) error) (err error) {
	ctx, cancel := context.WithTimeout(parent, s.DuckDBTimeout)
	defer cancel()
	release, err := s.acquireDuckDB(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := os.MkdirAll(s.DuckDBTempDir, 0700); err != nil {
		return err
	}
	if err := s.DiskCheck(s.DuckDBTempDir, s.Reserve); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(s.DuckDBTempDir, ".duckdb-file-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	diskErrors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := s.DiskCheck(tmp, s.Reserve); e != nil {
					diskErrors <- e
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-done
		select {
		case e := <-diskErrors:
			err = e
		default:
		}
	}()
	db, err := sql.Open("duckdb", s.duckDBMemoryDSN(tmp))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, `SET autoinstall_known_extensions=false; SET autoload_known_extensions=false`); err != nil {
		return err
	}
	return run(ctx, db)
}

func (s *Service) binaryExportJob(ctx context.Context, j jobs.Job, p convertPayload, progress func(int64, int64)) (string, error) {
	if p.CollectionID == "" {
		return "", fmt.Errorf("binary exports require an indexed collection")
	}
	c, err := s.Get(p.CollectionID)
	if err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return "", err
	}
	path := filepath.Join(s.DownloadDir, j.ID+"."+p.To)
	tmp := path + ".tmp"
	defer os.Remove(tmp)
	each := func(emit func(any) error) error {
		if p.ResultRef != nil {
			q := Query{}
			if p.Query != nil {
				q = *p.Query
			}
			return s.eachReference(ctx, *p.ResultRef, q, emit)
		}
		if p.Pipeline != nil {
			return s.eachPipeline(ctx, *p.Pipeline, p.Generations, emit)
		}
		if p.Query != nil {
			return s.EachQuery(ctx, c, *p.Query, p.Generation, emit)
		}
		if p.Options.ParquetTyped {
			return s.EachQuery(ctx, c, Query{}, c.Generation, emit)
		}
		return s.Each(ctx, c, emit)
	}
	var count int64
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count%1000 == 0 {
			progress(0, count)
			return s.DiskCheck(s.DownloadDir, s.Reserve)
		}
		return nil
	}
	if p.To == "xlsx" {
		fields, err := s.exportFields(c, p)
		if err != nil {
			return "", err
		}
		headers := []string{}
		for _, f := range fields {
			headers = append(headers, f.Name)
		}
		if p.Pipeline != nil {
			seen := map[string]map[string]bool{}
			if err = each(func(v any) error {
				for _, f := range Schema([]any{v}) {
					if seen[f.Name] == nil {
						if len(seen) >= 2048 {
							return fmt.Errorf("Excel supports at most 2048 columns")
						}
						seen[f.Name] = map[string]bool{"string": true}
					}
				}
				return nil
			}); err != nil {
				return "", err
			}
			headers = nil
			for _, f := range sortedFields(seen) {
				headers = append(headers, f.Name)
			}
		}
		if len(headers) == 0 || len(headers) > 2048 {
			return "", fmt.Errorf("Excel export requires object records with 1–2048 fields")
		}
		if err = os.MkdirAll(s.DuckDBTempDir, 0700); err != nil {
			return "", err
		}
		book := excelize.NewFile(excelize.Options{TmpDir: s.DuckDBTempDir})
		defer book.Close()
		stream, err := book.NewStreamWriter("Sheet1")
		if err != nil {
			return "", err
		}
		h := make([]any, len(headers))
		for i, key := range headers {
			h[i] = key
		}
		if err = stream.SetRow("A1", h); err != nil {
			return "", err
		}
		err = each(func(v any) error {
			if err := check(); err != nil {
				return err
			}
			if count >= 1048575 {
				return fmt.Errorf("Excel exceeds 1048575 data rows; use Parquet or NDJSON")
			}
			obj, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("Excel export requires object records")
			}
			values := make([]any, len(headers))
			for i, key := range headers {
				value := obj[key]
				if value == nil {
					values[i] = ""
				} else if text, ok := value.(string); ok {
					values[i] = text
				} else {
					b, e := conversion.MarshalJSON(value)
					if e != nil {
						return e
					}
					values[i] = string(b)
				}
				if len(fmt.Sprint(values[i])) > 32767 {
					return fmt.Errorf("Excel cell exceeds 32767 bytes; use Parquet")
				}
			}
			cell, _ := excelize.CoordinatesToCellName(1, int(count)+2)
			if e := stream.SetRow(cell, values); e != nil {
				return e
			}
			count++
			return nil
		})
		if err != nil {
			return "", err
		}
		if err = stream.Flush(); err != nil {
			return "", err
		}
		f, e := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		err = book.Write(&resources.Writer{W: f, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck})
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	} else {
		var typedColumns map[string]*parquetColumn
		if p.Options.ParquetTyped {
			typedColumns, err = inferParquetColumns(each)
			if err != nil {
				return "", err
			}
			if len(typedColumns) == 0 {
				if p.Pipeline != nil {
					return "", fmt.Errorf("Cannot infer typed columns from an empty pipeline result; choose JSON or lossless Parquet")
				}
				fields, e := s.exportFields(c, p)
				if e != nil {
					return "", e
				}
				for _, field := range fields {
					typedColumns[field.Name] = &parquetColumn{Kind: "text"}
				}
				if c.Format == "csv" && len(typedColumns) == 0 {
					source, e := os.Open(c.Path)
					if e != nil {
						return "", e
					}
					headers, e := conversion.CSVFields(source, conversion.Options{}, s.RecordBytes)
					source.Close()
					if e != nil {
						return "", e
					}
					for _, name := range headers {
						typedColumns[name] = &parquetColumn{Kind: "text"}
					}
				}
			}
		}
		input, err := os.CreateTemp(s.DownloadDir, ".parquet-input-*.ndjson")
		if err != nil {
			return "", err
		}
		defer os.Remove(input.Name())
		defer input.Close()
		w := &resources.Writer{W: input, Path: s.DownloadDir, Reserve: s.Reserve, Check: s.DiskCheck}
		err = each(func(v any) error {
			if e := check(); e != nil {
				return e
			}
			b, e := conversion.MarshalJSON(v)
			if e != nil {
				return e
			}
			var wrapped []byte
			if p.Options.ParquetTyped {
				obj, ok := v.(map[string]any)
				if !ok {
					return fmt.Errorf("Typed Parquet requires object records")
				}
				values := map[string]any{}
				for name := range typedColumns {
					value := obj[name]
					if value == nil {
						values[name] = nil
					} else if text, ok := value.(string); ok {
						values[name] = text
					} else {
						raw, err := conversion.MarshalJSON(value)
						if err != nil {
							return err
						}
						values[name] = string(raw)
					}
				}
				wrapped, e = json.Marshal(values)
			} else {
				wrapped, e = json.Marshal(map[string]string{"__fluxport_record_json_v1": string(b)})
			}
			if e != nil {
				return e
			}
			if _, e = w.Write(append(wrapped, '\n')); e != nil {
				return e
			}
			count++
			return nil
		})
		if err != nil {
			return "", err
		}
		if err = input.Close(); err != nil {
			return "", err
		}
		err = s.withBinaryDuckDB(ctx, func(ctx context.Context, db *sql.DB) error {
			query := `SELECT CAST(NULL AS VARCHAR) AS __fluxport_record_json_v1 WHERE false`
			if count > 0 {
				query = `SELECT * FROM read_ndjson(` + quoteSQL(input.Name()) + `,columns={'__fluxport_record_json_v1':'VARCHAR'},maximum_object_size=` + fmt.Sprint(s.RecordBytes*2+1024) + `)`
			}
			if p.Options.ParquetTyped {
				if count > 0 {
					var e error
					query, e = typedParquetSQL(input.Name(), typedColumns, s.RecordBytes)
					if e != nil {
						return e
					}
				} else {
					parts := []string{}
					for name, column := range typedColumns {
						parts = append(parts, "CAST(NULL AS "+parquetColumnType(column)+") AS "+quoteIdentifier(name))
					}
					if len(parts) == 0 {
						return fmt.Errorf("Typed Parquet requires named columns")
					}
					sort.Strings(parts)
					query = "SELECT " + strings.Join(parts, ",") + " WHERE false"
				}
			}
			_, e := db.ExecContext(ctx, `COPY (`+query+`) TO `+quoteSQL(tmp)+` (FORMAT PARQUET, COMPRESSION ZSTD)`)
			return e
		})
		if err != nil {
			return "", err
		}
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = s.DiskCheck(s.DownloadDir, s.Reserve); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	s.DB.Exec(`UPDATE jobs SET records=? WHERE id=?`, count, j.ID)
	progress(j.Total, count)
	return path, nil
}

func BinaryNotes(format string) string {
	if format == "xlsx" {
		return "Excel: choose a sheet to explore; text cells, raw date serials, formulas rejected. Export stores numbers and nested values as text; null, missing and empty text become blank cells. Original download preserves the complete workbook."
	}
	if format == "parquet" {
		return "Parquet: ordinary typed files import through DuckDB; dates become text. FluxPort exports use a lossless JSON-record column (__fluxport_record_json_v1) rather than an inferred analytical schema."
	}
	if format == "parquet-typed" {
		return "Typed Parquet: numbers and booleans become typed columns; CSV text stays text. Exact decimals use up to 38 digits. Larger or mixed values and nested objects become text. Missing and null merge into Parquet nulls. Use lossless Parquet to retain original record types and structure."
	}
	return ""
}
