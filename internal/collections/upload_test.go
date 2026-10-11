package collections

import (
	"os"
	"strings"
	"testing"
)

func TestUnsupportedUploadsNeverEnterDataDirectory(t *testing.T) {
	s := testService(t)
	for _, name := range []string{"nstalls.ini", "settings.exe", "data.json.ini", "README", "C:\\temp\\settings.INI"} {
		if _, err := s.Import(name, strings.NewReader("[settings]\ninstall=true")); err == nil {
			t.Fatalf("unsupported upload accepted: %s", name)
		}
		if _, err := s.StageConversion(name, strings.NewReader("{}")); err == nil {
			t.Fatalf("unsupported conversion upload accepted: %s", name)
		}
	}
	files, err := os.ReadDir(s.Root)
	if err != nil || len(files) != 0 {
		t.Fatal("unsupported files or staging files leaked", files, err)
	}
	_, total, err := s.List("", 1, 20)
	if err != nil || total != 0 {
		t.Fatal("unsupported collection persisted", total, err)
	}
}
