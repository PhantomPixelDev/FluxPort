package web

import (
	"strings"
	"testing"

	"fluxport/internal/collections"
	"fluxport/internal/transformations"
)

// Task 1 of docs/superpowers/plans/2026-10-17-ui-ux-simplification.md pins the
// single user vocabulary: files (not collections/datasets) and Saved steps
// (not pipelines/recipes). Each case renders one screen and requires the new
// copy present and the old copy absent. Extended after the branch review to
// cover the screens the first sweep missed.
func TestUserVocabularyUsesFilesTerms(t *testing.T) {
	app, _, col := setup(t, "", "")
	cases := []struct{ name, path, want, avoid string }{
		{name: "overview subtitle", path: "/overview", want: "Your files at a glance.", avoid: "Your collections at a glance"},
		{name: "overview file stat", path: "/overview", want: "<small>FILES</small>", avoid: "<small>COLLECTIONS</small>"},
		{name: "overview row stat", path: "/overview", want: "<small>ROWS</small>", avoid: "<small>RECORDS</small>"},
		{name: "sidebar convert item", path: "/collections", want: "Convert files", avoid: "Quick conversion"},
		{name: "sidebar sql item", path: "/collections", want: "SQL</a>", avoid: "SQL queries</a>"},
		{name: "sidebar status item", path: "/collections", want: "System status", avoid: "System overview"},
		{name: "empty search copy", path: "/collections?q=zzzznope", want: "No matching files", avoid: "No matching collections"},
		{name: "saved steps heading", path: "/pipelines?collection=" + col.ID, want: "Saved steps</h1>", avoid: "Data pipelines</h1>"},
		{name: "saved steps empty hint", path: "/pipelines?collection=" + col.ID, want: "Save these steps to run them again on another file.", avoid: "Save a recipe"},
		{name: "saved steps new button", path: "/pipelines?collection=" + col.ID, want: ">New saved steps<", avoid: "New pipeline"},
		{name: "saved steps parameters", path: "/pipelines?collection=" + col.ID, want: "Saved steps parameters", avoid: "Recipe parameters"},
		{name: "saved steps source", path: "/pipelines?collection=" + col.ID, want: "Source file", avoid: "Source collection"},
		{name: "saved steps search button", path: "/pipelines?collection=" + col.ID, want: "Find files", avoid: "Find collections"},
		{name: "saved steps append option", path: "/pipelines?collection=" + col.ID, want: "Append another file", avoid: "Append another collection"},
		{name: "saved steps run button", path: "/pipelines?collection=" + col.ID, want: "Save and run steps", avoid: "Save and run pipeline"},
		{name: "join builder option", path: "/collections/" + col.ID + "/changes", want: "All rows from this file", avoid: "from this dataset"},
		{name: "join builder summary", path: "/collections/" + col.ID + "/changes", want: "Combine with another file", avoid: "another dataset"},
		{name: "join builder search button", path: "/collections/" + col.ID + "/changes", want: ">Find a file<", avoid: "Find dataset"},
		{name: "sql browse link", path: "/sql", want: "Browse files", avoid: "Browse datasets"},
		{name: "sql picker title", path: "/sql", want: "Choose up to 8 files", avoid: "8 datasets"},
		{name: "workflow file picker", path: "/workflow", want: "Choose a file…", avoid: "Choose a collection"},
		{name: "workflow summary pointer", path: "/workflow", want: "Use Summary", avoid: "Use Inspect"},
		{name: "workflow api link", path: "/workflow?collection=" + col.ID, want: "this file's API", avoid: "this collection's API"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, html := call(t, app, "GET", tc.path, "", nil, nil)
			if !strings.Contains(html, tc.want) || strings.Contains(html, tc.avoid) {
				t.Fatalf("want %q present and %q absent", tc.want, tc.avoid)
			}
		})
	}
}

func TestSavedStepsDeleteButtonReadsSteps(t *testing.T) {
	app, service, col := setup(t, "", "")
	if _, err := service.SavePipeline(collections.Pipeline{Name: "Trim names", Source: col.ID, Steps: []collections.PipelineStep{{Op: "transform", Transform: &transformations.Operation{Kind: "trim", Field: "name"}}}}); err != nil {
		t.Fatal(err)
	}
	saved, err := service.Pipelines()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) == 0 {
		t.Fatal("pipeline was not saved")
	}
	_, html := call(t, app, "GET", "/pipelines?saved="+saved[0].ID, "", nil, nil)
	if !strings.Contains(html, ">Delete steps<") || strings.Contains(html, ">Delete recipe<") {
		t.Fatal("delete button does not read Delete steps")
	}
}
