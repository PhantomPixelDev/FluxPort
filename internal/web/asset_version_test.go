package web

import (
	"testing"
	"testing/fstest"
)

func TestFrontendVersionIncludesEveryStaticAsset(t *testing.T) {
	files := fstest.MapFS{
		"static/css/app.css":            {Data: []byte("base")},
		"static/css/file-workspace.css": {Data: []byte("workspace")},
		"static/js/file-workspace.js":   {Data: []byte("tools")},
		"static/css/quality.css":        {Data: []byte("quality")},
		"static/js/future-tool.js":      {Data: []byte("future")},
	}
	before, err := frontendAssetVersion(files)
	if err != nil {
		t.Fatal(err)
	}
	for name, file := range files {
		original := file.Data
		file.Data = append(append([]byte{}, original...), '!')
		after, err := frontendAssetVersion(files)
		if err != nil {
			t.Fatal(err)
		}
		if after == before {
			t.Errorf("changing %s did not invalidate cached assets", name)
		}
		file.Data = original
	}
	files["static/js/new-tool.js"] = &fstest.MapFile{Data: []byte("new")}
	after, err := frontendAssetVersion(files)
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Fatal("adding an asset did not invalidate cached assets")
	}
}
