package collections

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/xuri/excelize/v2"
	"os"
	"strings"
	"testing"
)

func TestTypedParquetExactColumnsAndFallbacks(t *testing.T) {
	s := testService(t)
	input := `[{"id":1,"big":123456789012345678901234567890123456,"price":1.234567890123456789,"huge":123456789012345678901234567890123456789,"flag":true,"nested":{"n":9007199254740993},"code":"001","mixed":1,"a.b/x'~":"山田"},{"id":2,"flag":false,"mixed":"yes","code":"002","price":null}]`
	c, err := s.Import("typed.json", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.ExportFile(context.Background(), c.ID, "parquet-typed", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	err = s.withBinaryDuckDB(context.Background(), func(ctx context.Context, db *sql.DB) error {
		var big, price, huge, nested, code, mixed, unicode string
		var flag bool
		q := `SELECT CAST(big AS VARCHAR),CAST(price AS VARCHAR),huge,nested,code,mixed,` + quoteIdentifier("a.b/x'~") + `,flag FROM read_parquet(` + quoteSQL(path) + `) WHERE id=1`
		if e := db.QueryRowContext(ctx, q).Scan(&big, &price, &huge, &nested, &code, &mixed, &unicode, &flag); e != nil {
			return e
		}
		if big != "123456789012345678901234567890123456" || price != "1.234567890123456789" || huge != "123456789012345678901234567890123456789" || nested != `{"n":9007199254740993}` || code != "001" || mixed != "1" || unicode != "山田" || !flag {
			t.Fatalf("values changed: %s %s %s %s %s %s %s %v", big, price, huge, nested, code, mixed, unicode, flag)
		}
		var typ string
		if e := db.QueryRowContext(ctx, `SELECT typeof(big) FROM read_parquet(`+quoteSQL(path)+`) LIMIT 1`).Scan(&typ); e != nil {
			return e
		}
		if typ != "DECIMAL(38,0)" {
			t.Fatal(typ)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.RawSource(c)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	before, err := os.ReadFile(c.Path)
	if err != nil || string(before) != input {
		t.Fatal("source changed", err)
	}
	csv, err := s.Import("codes.csv", strings.NewReader("code,value\n001,42\n"))
	if err != nil {
		t.Fatal(err)
	}
	path2, err := s.ExportFile(context.Background(), csv.ID, "parquet-typed", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path2)
	err = s.withBinaryDuckDB(context.Background(), func(ctx context.Context, db *sql.DB) error {
		var typ, code string
		e := db.QueryRowContext(ctx, `SELECT typeof(value),code FROM read_parquet(`+quoteSQL(path2)+`)`).Scan(&typ, &code)
		if e == nil && (typ != "VARCHAR" || code != "001") {
			t.Fatalf("CSV inferred: %s %s", typ, code)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTypedParquetScalarEmptyAndDecimalBounds(t *testing.T) {
	for _, text := range []string{"1e999999999", "123456789012345678901234567890123456789", "0.000000000000000000000000000000000000001"} {
		if _, _, _, ok := decimalShape(text); ok {
			t.Fatal("out-of-range number accepted", text)
		}
	}
	if _, scale, _, ok := decimalShape("0.12345678901234567890123456789012345678"); !ok || scale != 38 {
		t.Fatal("exact fractional number rejected")
	}
	s := testService(t)
	c, err := s.Import("scalar.json", strings.NewReader(`[1,"two"]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExportFile(context.Background(), c.ID, "parquet-typed", nil); err == nil {
		t.Fatal("scalar silently mapped")
	}
	empty, err := s.Import("empty.csv", strings.NewReader("id,name\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExportFile(context.Background(), empty.ID, "parquet-typed", nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbookSheetSelectionPreservesSourceAndReindexes(t *testing.T) {
	s := testService(t)
	s.MaxBytes = 1 << 20
	book := excelize.NewFile()
	defer book.Close()
	book.SetSheetRow("Sheet1", "A1", &[]any{"id", "name"})
	book.SetSheetRow("Sheet1", "A2", &[]any{"001", "Ada"})
	book.NewSheet("Other 山田")
	book.SetSheetRow("Other 山田", "A1", &[]any{"id", "name"})
	book.SetSheetRow("Other 山田", "A2", &[]any{"002", "Grace"})
	var data bytes.Buffer
	if err := book.Write(&data); err != nil {
		t.Fatal(err)
	}
	c, err := s.Import("sheets.xlsx", bytes.NewReader(data.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Workbook(c)
	if err != nil || len(info.Sheets) != 2 || info.Selected != "Sheet1" {
		t.Fatal(info, err)
	}
	if err = s.SelectWorkbookSheet(c.ID, "missing"); err == nil {
		t.Fatal("missing sheet accepted")
	}
	if err = s.SelectWorkbookSheet(c.ID, "Other 山田"); err != nil {
		t.Fatal(err)
	}
	next, err := s.Get(c.ID)
	if err != nil || next.Generation == c.Generation {
		t.Fatal("old generation reused", err)
	}
	rows, _, err := s.Page(context.Background(), next, 1, 20, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].(map[string]any)["id"] != "002" {
		t.Fatal(rows)
	}
	bytesOnDisk, err := os.ReadFile(c.Path)
	if err != nil || !bytes.Equal(bytesOnDisk, data.Bytes()) {
		t.Fatal("workbook source changed", err)
	}
	if s.Setting("workbook_sheet:"+c.Path, "") != "Other 山田" {
		t.Fatal("selection not persisted")
	}
}
