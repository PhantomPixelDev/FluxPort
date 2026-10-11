# Data explorer and SQL playground

Open a collection and choose **Open data explorer**. The original preview and API URLs continue to work.

## Explore and edit

Add filters for multiple fields and sort columns in priority order. Every filter must match. Dot paths address nested objects (`account.region`); JSON pointers address literal keys (`/a.b` addresses the key `a.b`). Equality keeps strings, booleans and numbers distinct. Missing values and explicit null both match **missing / null**. Numeric filtering uses SQLite numbers; integer filter values must fit signed 64-bit range.

Save the applied view to persist its filters, sorting, page size and visible columns. Resize headings by dragging their edge or focusing the separator and pressing the arrow keys. Browser preferences remember widths and visibility.

Enable CRUD in collection API configuration to edit an array-based source within the configured small-file limit. Sources with over 100,000 records remain protected from mutation. Double-click a cell, or focus it and press Enter, to edit. Enter saves; Escape cancels. Strings remain strings; other cells accept JSON. Long cells open the complete-record editor.

Select records to update fields, replace records or delete a batch. Updates merge top-level fields; null removes a field. Replacements require a complete record. Deletion asks for confirmation. A batch of up to 1,000 changes validates before one atomic source replacement. Large collections stay read-only.

Record identities survive edits through FluxPort, source reordering and subsequent refreshes. A scalar source `id` identifies a record; otherwise its normalized content identifies it. Occurrences distinguish duplicate IDs or values. An external edit without a business ID cannot reliably be matched to its previous value and may receive a new identity.

Edits require the index generation from the preview. A changed generation returns `409`, so stale selections cannot edit different rows. Background indexing completion updates pending explorer pages through server-sent events. A changed source prompts an already-open explorer to reload, preserving unsaved input.

**Prepare filtered download** exports the entire matching result, including all pages, in the selected order. Downloads run in existing cancellable background jobs. If the source changes before the job starts, the export fails rather than silently using different data.

## SQL

Choose up to eight collections in **SQL playground**. Each exposes a table named `c_<collection-id>`. Copy its identifier from **Dataset tables**. Fields are top-level columns; nested objects and arrays are JSON text.

    SELECT name, json_extract(account, '$.region') AS region
    FROM "c_your_collection_id"
    ORDER BY name
    LIMIT 100

Tables also expose `_record_id`, `_ordinal` and `_json`. `_json` contains the original normalized record, preserving its JSON number representation. Reserved metadata column names take precedence over source fields with the same name; those values remain available inside `_json`.

SELECT and WITH support joins, aggregates, filters and sorting. Ctrl/Cmd+Enter runs the query. Save queries for reuse, or publish them at `/api/queries/<query-id>`. The normal API authentication settings apply. Published queries require enabled collections without response transformations, preventing SQL from bypassing fields hidden by an endpoint's rules.

SQL runs on an isolated connection with read-only selected record indexes. FluxPort's internal metadata database is never attached. Multiple statements, writes, attachment commands, pragmas and filesystem or extension functions are rejected.

Limits: 10 seconds, eight datasets, 128 source fields per dataset, 128 result columns, 1,000 preview rows, 8 MiB total results, and 2 MiB per SQLite value/row. SQLite's statement, expression, compiled-program and compound-query limits are also reduced. Temporary sorting uses disk; the free-space reserve is checked before execution and while a query runs. Query history retains the last 200 executions; the UI displays the most recent 20.

SQLite numeric semantics apply: JSON booleans become SQL integers, and arithmetic on integers outside signed 64-bit range can lose precision. Use `_json` when exact original values are needed. BLOB results become base64 strings. Invalid UTF-8 text and non-finite numbers are rejected. Exports require a complete result; narrow or limit a truncated query. Give result columns unique aliases before exporting.

## API examples

    POST /api/collections/<id>/query
    Content-Type: application/json

    {"filters":[{"field":"active","op":"eq","value":true}],"sort":[{"field":"price","desc":true}],"page":1,"limit":50}

The response includes `records` with stable `id`, source `ordinal` and `value`, plus `generation`, `total`, `page` and `limit`.

    POST /api/collections/<id>/bulk
    Content-Type: application/json

    {"generation":"<generation from query>","changes":[{"id":"<record-id>","method":"PATCH","value":{"active":false}}]}

Methods are POST (append), PUT (replace), PATCH (merge), FIELD (set a dot-path or JSON-pointer field), and DELETE. FIELD uses `{"field":"account.region","value":"Europe"}`. Existing ordinal-based CRUD endpoints remain compatible.

Saved views use GET/POST `/api/collections/<id>/views` and DELETE `/api/collections/<id>/views/<view-id>`.

    POST /api/sql
    Content-Type: application/json

    {"collections":["<collection-id>"],"sql":"SELECT COUNT(*) AS count FROM \"c_<collection-id>\""}

SQL responses include `columns`, `rows`, `truncated`, `duration_ms` and selected dataset metadata.
