# Open a file, make changes, download the result

Start on **Your datasets**. Choose **Add files** to open JSON, CSV, YAML, XML, NDJSON, TOML, Excel or Parquet. Choose **Try an example** to load the small examples once. The home page opens this dataset list directly.

## Data

Open a dataset to search its rows. Choose Compact or Comfortable row density; desktop defaults to Compact, and touch layouts keep larger controls. Column headers show actual schema types and cycle sorting while preserving the rest of the query. Missing, null and empty text have different markers. JSON cells open the inspector, and Copy uses the complete value without rounding large numbers. Expand **Filter and sort** for more precise matching. **Columns** controls the visible fields; widths can be resized. **Save or download this view** remembers a query or exports all matching rows, rather than only the current page.

Inspect a row to see its complete JSON, including nested values. Optional **Edit the source file** enables the existing size-bounded editing path. Inline edits write to the original file; Changes creates a separate result. Large datasets remain read-only in the source editor.

## Changes

Choose an action and a column, then **Add change**. Leave Column empty for Remove duplicates to compare complete rows, or enter one column to keep the first row for each distinct value. Other actions trim spaces, change text case, fill empty values with text, rename/remove columns, or sort rows.

Before and after shows up to 20 rows per side. It is a sample, not a full dataset count. **Undo last change**, Remove and Clear changes edit the sequence. They do not modify the source file. Unsaved steps and their name can be restored in the same browser after navigating away; clearing browser storage removes those drafts.

**Save these steps** stores the sequence using the existing pipeline service. Saved names are shared across the instance; a name belonging to another dataset is rejected rather than replaced. Saved steps can be reopened from the dataset or the sidebar. The full editor remains available for joins, calculations, validation, revisions and schedules.

## Downloads

**Download result** runs the entire sequence over the complete dataset, saves the steps and opens Downloads with that export's status. Saving changes alone does not modify the source or prepare a download.

Downloads can export the original dataset or a selected set of saved changes. Original file streams the exact current bytes, even while indexing. Converted outputs show progress, cancellation, retry and the ready file. Excel uses text cells; Parquet stores complete records in one JSON column. Format details describes these mappings.

Cleanup and download forms work without JavaScript. JavaScript adds contextual controls, browser draft restoration and SSE progress. Without it, use Refresh download status for asynchronous exports. Source files retain their original contents unless you explicitly enable and use source editing or confirm source deletion.

## Compatibility

The collection IDs, APIs, authentication and CSRF protection remain unchanged. `/collections/:id/workbench` now opens the same Data workspace. Existing query/edit/view/export endpoints continue to work. The original system overview remains at `/overview`; `/workflow`, `/pipelines`, `/sql`, `/explorer` and converter URLs remain available.
