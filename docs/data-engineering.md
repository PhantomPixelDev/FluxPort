# Repeatable file processing

These workflows ship with FluxPort 0.3.24 and are available in the browser, REST API and Go CLI where noted below.

## Compare incoming files

Open the older file → **Inspect → Checks → Compare with another file**. Choose the newer file and key columns as a JSON array, for example `["customer.id","region/code"]`. Names are literal top-level names here. Keys must be present, non-null scalars and unique within each file. No arbitrary pairing of duplicate keys occurs.

Comparison joins complete immutable indexes on disk, reports exact added/removed/changed/unchanged counts and schema changes, and shows up to 20 differences within an 8 MiB sample budget. JSON object key ordering is normalized; scalar types and numeric lexemes remain significant (`1` and `1.0` differ). Missing, null and empty text remain different. Neither source is modified. Comparison has a two-minute timeout and uses the configured disk reserve.

API: `POST /api/collections/OLDER_ID/compare` with `{"other":"NEWER_ID","keys":["id"]}`. Both endpoints must be enabled and have no response transformations.

## Contracts and invalid rows

In **Checks → Data contract**, capture the current schema, then review expected types and whether columns are required, nullable or extra columns are allowed. Capture starts with required columns, nullable values and no extra columns. Contracts use literal names; existing validation rules still use dotted paths or JSON pointers.

```json
{"columns":[{"name":"id","types":["integer"],"required":true,"nullable":false},{"name":"email","types":["string"],"required":true,"nullable":true}],"allow_extra":false}
```

Checks and `POST /api/collections/ID/validate` use the saved contract. Ordinary quality scans remain bounded and clearly label partial coverage. Full pipeline exports and prepared cleanup results check their captured input against a saved contract before processing. A recipe can carry its own `input_contract`; final-output `validation` remains separate. An input contract failure stops publication. Quick previews do not establish full-input contract validity.

**Download checked records** scans the complete file in a cancellable background job. Choose **Stop on first invalid row** or **Separate invalid rows**. A successful ZIP contains:

- `valid.ndjson`: accepted records with exact normalized JSON values.
- `rejected.ndjson`: original records, source row numbers and validation reasons.
- `report.json`: source generation, mode, accepted count and validation totals.

The existing 10,000-distinct-value limit for each uniqueness rule still applies; exceeding it fails explicitly. Issue samples are bounded to 200. Sources remain unchanged. Failed/cancelled jobs publish no partial ZIP. Outputs expire after 24 hours and use existing job status, cancellation and retry controls.

API: GET/PUT/DELETE `/api/collections/ID/contract`; POST `/api/collections/ID/validation-export` with `{"mode":"separate"}` or `{"mode":"stop"}`. Use existing job status/download endpoints for the result. Checked-export APIs require an enabled endpoint without response transformations.

## Import interpretation

**Add files → Text import options** supports UTF-8, Windows-1252, UTF-16 LE/BE and comma/tab/semicolon/pipe/automatic CSV delimiters. Preview only reads up to 20 records and creates no collection; it does not validate the unscanned remainder. CSV values stay text, including leading zeros.

Default ordinary uploads preserve exact source bytes. Gzip text files are decompressed into the corresponding source format. Explicit non-default delimiter/encoding options create a clearly named `FILE-imported.ndjson` source. This normalization is intentional: the uploaded encoding/delimiter is not retained as the original download. XLSX/Parquet do not accept text normalization options. ZIP archive import is not supported.

```sh
fluxport import customers.csv.gz --encoding windows-1252 --delimiter semicolon
```

Date cleanup supports `timezone` and `output_timezone` as IANA names, with embedded timezone data for portable binaries. Defaults retain UTC behavior; explicit offsets in input are honored. Ambiguous/nonexistent local DST times follow Go's time parser, so prefer offset-bearing timestamps for such inputs.

```json
{"op":"transform","transform":{"kind":"date","field":"created","layout":"2006-01-02 15:04","output_layout":"2006-01-02T15:04:05Z07:00","timezone":"Europe/Berlin","output_timezone":"UTC"}}
```

## Portable recipes and execution reports

Existing definition import/export now includes `parameters`, `input_query`, optional `input_contract` and output `validation`. Keep the JSON definition in Git. Each parameter needs a default; replace values with explicit `{"$parameter":"name"}` nodes. Parameters substitute JSON values in steps, never SQL fragments, identifiers or substrings.

```json
{"version":1,"name":"Regional customers","source":"COLLECTION_ID","parameters":{"region":"DE"},"steps":[{"op":"filter","filters":[{"field":"country","op":"eq","value":{"$parameter":"region"}}]}]}
```

```sh
fluxport pipeline run recipe.json --source COLLECTION_ID --params '{"region":"GB"}' --format parquet --output customers.parquet
```

CLI runs accept a saved recipe ID or a JSON definition up to 64 KiB. API `POST /api/pipelines/ID/run` accepts optional `parameters` and the existing source override. Unknown override names, missing defaults, oversized values and invalid expanded steps are errors. Existing recipes need no changes.

From saved-recipe settings, **Prepare execution report** queues a background audit. The downloadable JSON records the full recipe/revision, source generations, rows in/out and duration per step. Audit counts recompute each prefix; they are not a single-pass benchmark, and they do not replace final-output validation. Failures identify the step in job status. Normal export jobs already retain the submitted recipe and dependency generations. Audit jobs appear in recipe execution history and expire after 24 hours.

API: POST `/api/pipelines/ID/audit`. CLI operations continue to require a separate workspace from a concurrently running server.

## SQL assistance

The SQL view includes a searchable table/column picker with safe identifier quoting, named scalar parameters and **Explain query**. Native SQLite uses `EXPLAIN QUERY PLAN`; Automatic/DuckDB explanation uses DuckDB `EXPLAIN` without running `ANALYZE`. Existing query timeout, filesystem isolation and result limits remain active. The SQL engine's normal numeric behavior still applies; `_json` retains original normalized values.

Use `:start_date` outside quotes and supply a parameter object. Quotes and comments are left intact. Missing/unused parameters fail clearly. Save query captures supplied values; workspace session restoration preserves text, parameters and engine without executing anything.

```sh
fluxport query --collections COLLECTION_ID --engine duckdb --sql 'SELECT count(*) FROM c_COLLECTION_ID WHERE country=:country' --params '{"country":"DE"}' --explain
```

`POST /api/sql` accepts `parameters` and optional `explain:true` alongside the existing fields.

## Reshape nested data

Advanced cleanup steps support these operations through the normal preview, saved recipe, CLI and export paths:

```json
{"op":"explode","nested":{"field":"items","as":"item"}}
{"op":"unpivot","nested":{"columns":["january","february"],"as":"month","value_as":"amount"}}
{"op":"pivot","keys":["customer"],"nested":{"field":"month","value":"amount","columns":["january","february"]}}
```

Names are literal top-level names. Explode omits missing/null/empty arrays, rejects other types and limits each array to 10,000 items. Unpivot omits missing columns while retaining explicit nulls; unrelated columns remain. Both reject target collisions and bound each expanded source record to 1 MiB.

Pivot requires explicit output columns, text names and one value per grouping key/output column. Duplicate or undeclared cells fail rather than silently aggregating. Missing cells become null; non-group columns are dropped. Grouping distinguishes missing and null inputs, although the resulting JSON grouping fields represent either as null. Existing flatten remains available and rejects name collisions. Up to 128 pivot/unpivot columns and 16 grouping keys are supported. Large integer and nested JSON value payloads remain exact.
