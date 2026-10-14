# Open a file, make changes, download the result

Opening a file keeps you in one workspace. The table, **Clean** and **Convert / Download** share the same collection page. Desktop displays SQL and all other tools in the toolbar; mobile keeps them under **⋯ More**. Icons and accessible names identify each tool. On desktop, tools open beside the main table; on smaller screens they open below it. Switching tools and reloading the same browser tab retain view state and SQL text/engine choice. Restored SQL never runs automatically. Cleanup drafts also retain their captured input scope and flag source changes. **×** or Escape returns to the full-width table. Save steps or queries to share or reuse them.

The SQL panel starts with this file selected, and its saved queries and history belong to this file. Summaries, validation, raw JSON, schema and API settings are in the desktop toolbar or **⋯ More** on mobile. File settings, processing history and saved views are there too. **Files** returns to the file list. Native links and forms remain available when JavaScript is disabled.

Open **Column summaries** (under **⋯ More** on mobile) for column statistics and cleanup suggestions. To show type labels in the table, enable **⚙ Table options → Show column types**. The default scan checks up to 10,000 source rows; missing, null and empty text stay distinct. Common values track at most 64 values per column and display the five most frequent tracked values. Distinct counting retains at most 1,024 values, and duplicate tracking retains at most 10,000 row identities. Capped counts and sampled coverage are labeled. Wide datasets can be summarized one column at a time.

Suggestions offer previews of removing identical rows or trimming whitespace in complete text columns. They start a new set of changes and never alter the source. Nothing is applied automatically.

In **Clean → Advanced**, expand **Combine with another dataset**, select matching columns, choose whether to keep all left rows or only matches, and name a new output column. The matched row becomes a nested object; multiple matches expand output rows. Preview reports matched, unmatched and multiple-match counts for up to 20 input rows, not full-dataset totals. Search dataset names when the picker exceeds 100 matching datasets.

**Clean → Advanced → Reuse steps from another dataset** copies a recipe onto the current source with a separate name. Preview before saving; incompatible columns or types must be corrected. The original recipe and source remain unchanged.

Excel datasets show the selected worksheet in Data. **Open sheet** rebuilds the index for that worksheet; it does not rewrite the workbook. Original downloads include all sheets. Formulas remain rejected throughout the workbook.

Start on **Your datasets**. Choose **Add files** to open JSON, CSV, YAML, XML, NDJSON, TOML, Excel or Parquet. Choose **Try an example** to load the small examples once. The home page opens this dataset list directly.

## Data

Open a dataset to search its rows. Open **⚙ Table options** to choose Compact or Comfortable row density; desktop defaults to Compact, and touch layouts keep larger controls. Column headers cycle sorting while preserving the rest of the query. Missing, null and empty text have different markers. JSON cells open the inspector, and Copy uses the complete value without rounding large numbers. Expand **Filter** for more precise matching. **⚙ Table options** controls the visible fields; widths can be resized. **Saved views** remembers source queries; filtered downloads are in **Convert / Download**.

Inspect a row to see its complete JSON, including nested values. **File settings → Edit the source file** (under **⋯ More** on mobile) enables the existing size-bounded editing path. Inline edits write to the original file; Changes creates a separate result. Large datasets remain read-only in the source editor.

## Clean

Choose a change and a column, then **Apply**. Choose **All columns** for Remove duplicates to compare complete rows, or select one column to keep the first row for each distinct value. Other actions trim spaces, change text case, fill empty values with text, rename/remove columns, or sort rows.

**Input → Matching rows** captures all matching source rows, across every page, in the current sort order. **Entire file** includes the full source. This captured input stays fixed when you add more steps or filter the Cleaned table. **Update input** explicitly recaptures the current Original view and regenerates the result.

**Original** shows the source. **Cleaned** initially shows a labeled quick sample of up to 20 rows while a background job prepares the full result. Once ready, use the same search, filters, sorting, column controls, inspector and numbered pagination on every cleaned row. Exact counts appear when preparation completes. Cleaned data stays read-only, and column choices follow renamed/output fields. A sample is neither a total nor a positional comparison after sorting/deduplication. On mobile, **Back to changes** returns from the sample to the controls. **Undo**, **× Remove** and **Advanced → Reset** edit the sequence without modifying the source.

**Advanced → Save these steps** stores the sequence using the existing pipeline service. Saved names are shared across the instance; a name belonging to another dataset is rejected rather than replaced. Saved steps can be reopened from the dataset or the sidebar. The full editor remains available for joins, calculations, validation, revisions and schedules.

## Convert / Download

Open **Convert / Download** and pick a **Format**. Click **Download** for the original file, or **Convert & download** for another format. It defaults to the original or cleaned dataset currently shown and **All matching rows**, across every page, preserving its sort. Choose **Entire dataset** to omit browsing filters. When multiple choices exist, **Change** lets you choose the original data, current cleaned data or saved cleanup. Current cleanup does not need to be saved before downloading. Saving a recipe is an independent option.

Click **Download** once. Original file streams the exact source bytes, even while indexing. Converted files reuse prepared cleaned data. Exports submitted during preparation show **Waiting for cleaned data** without using a worker, then **Creating your file**, and download automatically when ready. **Download again** remains available if the browser blocks automatic downloads or you want another copy. **Another format** returns to the chooser. Progress uses SSE; cancellation and retry remain available.

Excel uses text cells. Parquet offers lossless JSON records or typed analytical columns. **Format details** contains the full mapping and an optional **Check format changes** sample check. Downloads use the source display name and the output extension. Cleaned exports add `-cleaned`; filtered views add `-filtered`. Disposable job IDs stay out of public filenames.

Incompatible exports show an error instead of publishing an incomplete file.

Cleanup and download forms work without JavaScript. JavaScript adds contextual controls, browser draft restoration and SSE progress. Without it, use **Refresh status** to browse complete cleaned rows and **Refresh download status** for exports. Source files retain their original contents unless you explicitly enable and use source editing or confirm source deletion.

Prepared results are disposable and expire after 24 hours. They live outside the source folder and never appear as additional collections. A changed source requires preparing again. Collection history contains only that file’s jobs; library history remains global.

## Compatibility

The collection IDs, APIs, authentication and CSRF protection remain unchanged. `/collections/:id/workbench` now opens the same Data workspace. Existing query/edit/view/export endpoints continue to work. The original system overview remains at `/overview`; `/workflow`, `/pipelines`, `/sql`, `/explorer` and converter URLs remain available.
