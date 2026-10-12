//go:build ignore

package main

import (
	"github.com/xuri/excelize/v2"
	"log"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: browser-fixtures OUTPUT_DIR")
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Fatal(err)
	}
	book := excelize.NewFile()
	defer book.Close()
	book.SetSheetRow("Sheet1", "A1", &[]any{"code", "label"})
	book.SetSheetRow("Sheet1", "A2", &[]any{"001", "First sheet"})
	book.NewSheet("Second 山田")
	book.SetSheetRow("Second 山田", "A1", &[]any{"code", "label"})
	book.SetSheetRow("Second 山田", "A2", &[]any{"002", "Second sheet"})
	if err := book.SaveAs(filepath.Join(dir, "multi-sheet.xlsx")); err != nil {
		log.Fatal(err)
	}
}
