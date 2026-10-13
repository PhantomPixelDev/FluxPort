# Open a file, make changes, download the result

Opening a file keeps you in one workspace. The table, **Clean** and **Download** share the same collection page. SQL and other tools are in **⋯ More**. On desktop, tools open beside the main table; on smaller screens they open below it. Switching tools retains your cleanup and SQL drafts for the lifetime of the page. **×** or Escape returns to the full-width table. Save steps or queries to keep them beyond this browser page.

The SQL panel starts with this file selected, and its saved queries and history belong to this file. Summaries, validation, raw JSON, schema and API settings are under **⋯ More**. File settings, processing history and saved views are there too. **Files** returns to the file list. Native links and forms remain available when JavaScript is disabled.

Open **⋯ More → Column summaries** for column statistics and cleanup suggestions. To show type labels in the table, enable **⚙ Table options → Show column types**. The default scan checks up to 10,000 source rows; missing, null and empty text stay distinct. Common values track at most 64 values per column and display the five most frequent tracked values. Distinct counting retains at most 1,024 values, and duplicate tracking retains at most 10,000 row identities. Capped counts and sampled coverage are labeled. Wide datasets can be summarized one column at a time.

Suggestions offer previews of removing identical rows or trimming whitespace in complete text columns. They start a new set of changes and never alter the source. Nothing is applied automatically.

In **Clean → Advanced**, expand **Combine with another dataset**, select matching columns, choose whether to keep all left rows or only matches, and name a new output column. The matched row becomes a nested object; multiple matches expand output rows. Preview reports matched, unmatched and multiple-match counts for up to 20 input rows, not full-dataset totals. Search dataset names when the picker exceeds 100 matching datasets.

**Clean → Advanced → Reuse steps from another dataset** copies a recipe onto the current source with a separate name. Preview before saving; incompatible columns or types must be corrected. The original recipe and source remain unchanged.

Excel datasets show the selected worksheet in Data. **Open sheet** rebuilds the index for that worksheet; it does not rewrite the workbook. Original downloads include all sheets. Formulas remain rejected throughout the workbook.

Start on **Your datasets**. Choose **Add files** to open JSON, CSV, YAML, XML, NDJSON, TOML, Excel or Parquet. Choose **Try an example** to load the small examples once. The home page opens this dataset list directly.

## Data

Open a dataset to search its rows. Open **⚙ Table options** to choose Compact or Comfortable row density; desktop defaults to Compact, and touch layouts keep larger controls. Column headers cycle sorting while preserving the rest of the query. Missing, null and empty text have different markers. JSON cells open the inspector, and Copy uses the complete value without rounding large numbers. Expand **Filter** for more precise matching. **⚙ Table options** controls the visible fields; widths can be resized. **⋯ More → Saved views & filtered download** remembers a query or exports all matching rows, rather than only the current page.

Inspect a row to see its complete JSON, including nested values. **⋯ More → File settings → Edit the source file** enables the existing size-bounded editing path. Inline edits write to the original file; Changes creates a separate result. Large datasets remain read-only in the source editor.

## Clean

Choose a change and a column, then **Apply**. Choose **All columns** for Remove duplicates to compare complete rows, or select one column to keep the first row for each distinct value. Other actions trim spaces, change text case, fill empty values with text, rename/remove columns, or sort rows.

Each added change updates the main table. **Original** shows the source with normal pagination; **Cleaned** shows a sample of up to 20 output rows. The sample is not a full result count or a positional comparison after sorting/deduplication. Downloads run all steps on the complete dataset. On mobile, **Back to changes** returns from the sample to the controls. **Undo**, **× Remove** and **Advanced → Reset** edit the sequence without modifying the source.

**Advanced → Save these steps** stores the sequence using the existing pipeline service. Saved names are shared across the instance; a name belonging to another dataset is rejected rather than replaced. Saved steps can be reopened from the dataset or the sidebar. The full editor remains available for joins, calculations, validation, revisions and schedules.

## Download

Open **Download**, pick a **Format**, and click **Download**. It defaults to the original or cleaned data currently shown in the table. When multiple choices exist, **Change** lets you choose the original data, current cleaned data or saved cleanup. Current cleanup does not need to be saved before downloading. Saving a recipe is an independent option.

Click **Download** once. Original file streams the exact source bytes, even while indexing. Converted files show **Creating your file**, then start downloading automatically when ready. **Download again** remains available if the browser blocks automatic downloads or you want another copy. **Another format** returns to the chooser. Progress uses SSE; cancellation and retry remain available.

Excel uses text cells. Parquet offers lossless JSON records or typed analytical columns. **Options → Format details** contains the full mapping and an optional **Check format changes** sample check. Downloads use the source display name and the output extension. Cleaned exports add `-cleaned`; filtered views add `-filtered`. Disposable job IDs stay out of public filenames.\n\nIncompatible exports show an error instead of publishing an incomplete file.

Cleanup and download forms work without JavaScript. JavaScript adds contextual controls, browser draft restoration and SSE progress. Without it, use Refresh download status for asynchronous exports. Source files retain their original contents unless you explicitly enable and use source editing or confirm source deletion.

## Compatibility

The collection IDs, APIs, authentication and CSRF protection remain unchanged. `/collections/:id/workbench` now opens the same Data workspace. Existing query/edit/view/export endpoints continue to work. The original system overview remains at `/overview`; `/workflow`, `/pipelines`, `/sql`, `/explorer` and converter URLs remain available.
