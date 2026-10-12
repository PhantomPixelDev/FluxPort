package collections

import (
	"fmt"
	"github.com/xuri/excelize/v2"
	"os"
)

type WorkbookInfo struct {
	Sheets   []string
	Selected string
}

func (s *Service) Workbook(c Collection) (WorkbookInfo, error) {
	var out WorkbookInfo
	if c.Format != "xlsx" {
		return out, fmt.Errorf("Choose an Excel dataset")
	}
	if err := s.safe(c.Path); err != nil {
		return out, err
	}
	info, err := os.Stat(c.Path)
	if err != nil {
		return out, err
	}
	if info.Size() > s.MaxBytes {
		return out, ErrTooLarge
	}
	book, err := excelize.OpenFile(c.Path, excelize.Options{RawCellValue: true, UnzipSizeLimit: 64 << 20, UnzipXMLSizeLimit: 4 << 20, TmpDir: s.IndexDir})
	if err != nil {
		return out, err
	}
	defer book.Close()
	out.Sheets = book.GetSheetList()
	if len(out.Sheets) == 0 {
		return out, fmt.Errorf("Workbook contains no sheets")
	}
	out.Selected = s.Setting("workbook_sheet:"+c.Path, out.Sheets[0])
	return out, nil
}
func (s *Service) SelectWorkbookSheet(id, sheet string) error {
	c, err := s.Get(id)
	if err != nil {
		return err
	}
	unlock := s.lock(c.Path)
	defer unlock()
	info, err := s.Workbook(c)
	if err != nil {
		return err
	}
	found := false
	for _, name := range info.Sheets {
		if name == sheet {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("Sheet not found in this workbook")
	}
	if info.Selected == sheet {
		if c.Status == "failed" || c.Status == "cancelled" || c.Status == "interrupted" {
			return s.syncFile(c.Path, true)
		}
		return nil
	}
	if err = s.Jobs.CancelResource(c.ID); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "workbook_sheet:"+c.Path, sheet); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE collections SET index_limits='',status='pending',error='' WHERE id=?`, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.syncFile(c.Path, true)
}
