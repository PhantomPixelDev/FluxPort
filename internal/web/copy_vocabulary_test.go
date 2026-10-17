package web

import (
	"strings"
	"testing"
)

// Task 1 of docs/superpowers/plans/2026-10-17-ui-ux-simplification.md pins the
// single user vocabulary: files (not collections/datasets) and Saved steps
// (not pipelines/recipes). Each case renders one screen and requires the new
// copy present and the old copy absent.
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
