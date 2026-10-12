package collections

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxport/internal/conversion"
	"github.com/xuri/excelize/v2"
)

func TestParquetImportAndLosslessExport(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 1 << 20
	c, err := s.Import("nested.json", strings.NewReader(`[{"id":9007199254740993,"nested":{"a":[true,null,"你好"]}},{"id":123456789012345678901234567890,"value":1.234567890123456789}]`))
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ExportJob(c.ID, "parquet")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.convertJob(context.Background(), job, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	imported, err := s.Import("export.parquet", f)
	if err != nil {
		t.Fatal(err)
	}
	if !imported.ReadOnly || imported.Rows != 2 {
		t.Fatal(imported)
	}
	before, _, err := s.Page(context.Background(), c, 1, 10, false, "")
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := s.Page(context.Background(), imported, 1, 10, false, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := conversion.MarshalJSON(before)
	a, _ := conversion.MarshalJSON(after)
	if !bytes.Equal(a, b) {
		t.Fatalf("fidelity changed: %s vs %s", a, b)
	}
	ordinary := filepath.Join(s.Root, "ordinary.parquet")
	err = s.withBinaryDuckDB(context.Background(), func(ctx context.Context, db *sql.DB) error {
		_, e := db.ExecContext(ctx, `COPY (SELECT 42::BIGINT AS id, 'Ada' AS name, 12.345::DECIMAL(8,3) AS price) TO `+quoteSQL(ordinary)+` (FORMAT PARQUET)`)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SyncFile(ordinary); err != nil {
		t.Fatal(err)
	}
	normal, err := s.byPath(ordinary)
	if err != nil || normal.Rows != 1 || normal.Status != "ready" {
		t.Fatal(normal, err)
	}
}
func TestExcelTextCellsFormulaAndSheetLimits(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 1 << 20
	c, err := s.Import("rows.json", strings.NewReader(`[{"id":9007199254740993,"name":"=SUM(A1:A2)","nested":{"x":true}}]`))
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ExportJob(c.ID, "xlsx")
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.convertJob(context.Background(), job, func(int64, int64) {})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	col, err := s.Import("rows.xlsx", f)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := s.Page(context.Background(), col, 1, 10, false, "")
	if err != nil {
		t.Fatal(err)
	}
	obj := rows[0].(map[string]any)
	if obj["id"] != "9007199254740993" || obj["name"] != "=SUM(A1:A2)" || obj["nested"] != `{"x":true}` {
		t.Fatal(obj)
	}
	book := excelize.NewFile()
	defer book.Close()
	book.SetCellValue("Sheet1", "A1", "id")
	book.SetCellFormula("Sheet1", "A2", "1+1")
	var data bytes.Buffer
	book.Write(&data)
	if _, err = s.Import("formula.xlsx", bytes.NewReader(data.Bytes())); err == nil {
		t.Fatal("formula accepted")
	}
	book.NewSheet("Extra")
	data.Reset()
	book.Write(&data)
	if _, err = s.Import("multisheet.xlsx", bytes.NewReader(data.Bytes())); err == nil {
		t.Fatal("formula-bearing workbook accepted after adding another sheet")
	}
}
func TestBinaryMalformedCancellationAndDiskFailure(t *testing.T) {
	s := testService(t)
	if _, err := s.Import("broken.parquet", strings.NewReader("not parquet")); err == nil {
		t.Fatal("invalid parquet accepted")
	}
	if _, err := s.Import("broken.xlsx", strings.NewReader("not a zip")); err == nil {
		t.Fatal("invalid excel accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.withBinaryDuckDB(ctx, func(ctx context.Context, db *sql.DB) error { _, e := db.ExecContext(ctx, "SELECT 1"); return e }); err == nil {
		t.Fatal("cancel ignored")
	}
	c, err := s.Import("source.json", strings.NewReader(`[{"id":1}]`))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(c.Path)
	s.DiskCheck = func(string, uint64) error { return errors.New("test low disk") }
	if _, err = s.ExportFile(context.Background(), c.ID, "parquet", nil); err == nil {
		t.Fatal("low disk ignored")
	}
	after, _ := os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("source modified on export failure")
	}
}

func TestExcelExpandedArchiveBound(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 1 << 20
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, err := writer.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte{'x'}, 1<<20)
	for i := 0; i < 65; i++ {
		if _, err = entry.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Import("bomb.xlsx", bytes.NewReader(data.Bytes())); err == nil || !strings.Contains(err.Error(), "expanded size") {
		t.Fatal("expanded archive bound ignored", err)
	}
}
