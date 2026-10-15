# Work on a file

Open a file from **Files**. **Data · Clean · SQL · Inspect** stay within that file. Tables, editors and results use the main page. **Convert / Download** is always in the header; **File settings** contains source management, API settings and saved views.

## Edit and browse

Search, filter, sort, resize or hide columns in **Data**. Numbered pagination browses the complete file. Comfortable is the default density; saved Compact preferences are retained. Missing, null and empty text have distinct markers. Copy preserves complete values, including large integers.

Double-click a cell, or focus it and press Enter, to edit. Enter saves; Escape cancels. JSON objects, arrays, long values and scalar documents open a larger editor. JSON edits are validated without rounding numbers. Strings in CSV remain text. Selected rows can also be updated, replaced or deleted through **Edit selected rows**; deletion requires confirmation.

Direct editing supports text formats within the configured buffered-file limit (16 MiB by default) and 100,000 rows. Excel, Parquet and larger files stay read-only. Editing the dashboard file does not enable the public REST API or its CRUD flag.

## Undo and history

**Undo** and **Redo** restore saved file contents. Ctrl/Cmd+Z and Ctrl/Cmd+Shift+Z work outside text inputs; inputs retain native editing shortcuts. **History** lists saved versions and lets you restore one as a new change. The newest versions are retained, up to 20 saves or 64 MiB per file. The baseline counts as one version.

History persists in the state directory. Keep both source and state volumes in backups. Source writes are atomic and journaled; startup reconciles interrupted writes without rewriting the source. Undo restores exact saved bytes and record identities. New edits after Undo discard the redo branch. External source changes require a reload. History is recovery for recent work, not a substitute for backups.

## Clean

Choose **Rows to clean**, a change, its column and **Apply change**. The file table stays below the controls. For editable files, Apply saves to the same file; Undo restores the previous version. File formatting may be normalized when writing.

**Matching rows** includes every match across all pages. Text and column changes preserve unmatched rows and source positions. Explicit sort, join and row-producing operations place their output into the matched positions, retaining unrelated rows. **Entire file** processes every row. The input query is captured explicitly; **Use current filter** recaptures it.

Saved recipes and advanced joins remain available. Loading a recipe previews its steps; **Apply these steps to file** explicitly saves them. Nothing runs merely from restoring a view or SQL draft.

For read-only files, cleanup prepares a separate working preview for download. The preview replaces the visible table rather than introducing Original/Cleaned tabs. Full prepared results provide filtering, sorting and pagination after the quick sample. They expire after 24 hours and never appear as extra collections. A changed source requires preparing again.

## SQL and inspection

SQL queries the current source file. The editor is followed by Run, engine selection and results. Saved queries/history remain collapsed; restoring SQL text never executes it. SQL exports use **Run and download** and retain the existing result limits.

**Inspect → Summary** is one column table. Expand a row to see statistics and common values. Checks, Schema and JSON have their own full-width views. Summary scans are bounded and label sample coverage, distinct-count limits and tracked values; missing, null and empty text remain separate.

## Convert / Download

Choose **Data**, **Rows** and **Format**, then Download. File data uses current saved contents. All matching rows includes every page in the displayed sort order; Entire dataset omits browsing filters. Original file streams exact current source bytes, including all Excel sheets.

Read-only cleanup previews and saved steps remain available as download sources. Background exports show preparation, failure/retry and the ready file. SSE updates progress without polling. Without JavaScript, native links/forms work and provide manual status refresh.

Excel saves text cells; Parquet offers lossless JSON records or typed analytical columns. **Format details** explains mappings; incompatible conversions fail explicitly. Public filenames use the display name rather than disposable job IDs.

## Compatibility and recovery

Existing collection IDs, authentication, CSRF, APIs and query semantics remain intact. Legacy pipeline requests without the file-edit option keep their preview/export behavior. REST mutations still require their existing flags. Source deletion remains a separately confirmed action.

File history uses metadata schema 10. Back up source and state together before upgrading; restoring an older application requires the matching pre-upgrade state backup. Processing history is collection-specific; library history is global.
