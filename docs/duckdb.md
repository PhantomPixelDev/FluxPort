# DuckDB processing

FluxPort keeps SQLite for application metadata, saved queries, and the native SQL path. DuckDB is included in the standard build and opened only when automatic or manual engine selection routes an analytical query to it. It supports joins, aggregates, filters, and sorts over normalized collection records. Simple conversions remain on the existing Go codecs.

## Build

Use the standard build command to include DuckDB:

```sh
go build -trimpath -o fluxport ./cmd/fluxport
```

Install a C compiler before building. The driver bundles its native libraries for supported targets:

```sh
CGO_ENABLED=1 go build -trimpath -o fluxport ./cmd/fluxport
```

The driver currently bundles libraries for Windows amd64, Linux amd64/arm64, and macOS amd64/arm64. DuckDB increases binary size substantially. FluxPort still starts without initializing the analytical engine until a query needs it.

## Engine selection

The SQL Playground and `POST /api/sql` accept `engine: "auto"`, `"native"`, or `"duckdb"`. Omit it to use `auto`. Auto keeps simple/small queries on SQLite and selects DuckDB for joins, filters, sorts, or aggregates when the selected ready collections total at least 250,000 indexed records. The first DuckDB query builds its generation cache; choose `native` to avoid that cost for a one-off query. The result includes the engine used. Saved queries use automatic selection when executed.

DuckDB is initialized only for a query that selects it. The first use of each immutable collection generation copies its normalized SQLite index to a disk-backed DuckDB cache; later queries reuse that cache. A changed source gets a new collection generation and therefore a new cache. Source files and SQLite indexes remain the source of truth. Cache creation requires additional disk space proportional to the selected dataset.

The initial integration connects DuckDB to the SQL Playground and SQL API; collection details include a shortcut that opens the selected dataset in the playground. Existing Data Explorer pagination and batch/pipeline transformations remain on their established SQLite/Go paths to preserve their filtering and conversion semantics. CSV collections can be queried after their normal import/index step. Direct Parquet import or user-provided Parquet scans are not enabled yet.

## Limits and configuration

Defaults are 512 MiB DuckDB memory, up to 4 GiB of DuckDB spill storage, a 30-second query timeout, two DuckDB execution threads, 1,000 preview rows, and an 8 MiB result cap. DuckDB spills intermediate work to disk, but it still needs free disk space. FluxPort reserves the configured minimum free-space amount before and during cache creation and query execution. Each configured DuckDB memory limit must leave room for the rest of the Go process and OS.

Set these values in the JSON config or environment:

| Environment variable | Default | Valid range |
| --- | ---: | ---: |
| `FLUXPORT_DUCKDB_MEMORY_BYTES` | 536870912 | 64 MiB–1 TiB |
| `FLUXPORT_DUCKDB_TEMP_BYTES` | 4294967296 | 1 GiB–1 PiB |
| `FLUXPORT_DUCKDB_TIMEOUT_SECONDS` | 30 | 1–900 |
| `FLUXPORT_DUCKDB_TEMP_DIR` | `.fluxport/duckdb-tmp` | Any writable directory |

For example, set `FLUXPORT_DUCKDB_TEMP_DIR` to a fast local disk with adequate free space. Keep this directory private to the FluxPort service account.

## SQL isolation

Queries accept only one read-only `SELECT` or `WITH` statement. The query connection contains only the explicitly selected, read-only, generation-keyed DuckDB caches. After attaching those files, FluxPort disables DuckDB external filesystem access. File-reading table functions, catalog inspection, extension loading, settings changes, and write statements are rejected. Query cancellation is tied to the HTTP request context and timeout. These controls are defense in depth; run FluxPort with OS-level access limited to its data and state directories.

DuckDB's SQL types and optimizer can produce different numeric behavior from SQLite. Cast explicitly when comparing mixed schema fields. `_json` remains available for the original normalized record value. Parquet files are not an import format yet; DuckDB's built-in Parquet scans are intentionally unavailable to user SQL in this isolated integration.

## Tests and benchmark

Run the full test suite with `CGO_ENABLED=1 go test ./...`. DuckDB coverage includes a cross-collection join, aggregation, query isolation, cache refresh after source generation changes, cancellation, and bounded-memory sorting.

Compare warmed-up aggregate latency and the first query, including cache creation, with:

```sh
    CGO_ENABLED=1 go test ./internal/collections -run '^$' -bench 'BenchmarkSQL(Native|DuckDB)' -benchtime=1x
```

The aggregate benchmarks warm the cache before timing, while `BenchmarkSQLDuckDBFirstQuery` includes cache creation. Results depend on dataset shape and disk speed; compare both when evaluating a one-off job and a repeated analytical workload.

### Measured Windows result

On Windows amd64 with an AMD Ryzen 5 PRO 4650U, a single 250,000-record grouped aggregate measured 717 ms on native SQLite and 158 ms on warmed DuckDB. The first DuckDB query, including its cache build, measured 1,241 ms. The warmed DuckDB query allocated about 156 KB in Go versus 542 KB for SQLite; this is Go allocation volume, not total DuckDB/native peak memory. A separate 5-million-row sort passed with DuckDB capped at 64 MiB and a 1 GiB temporary-storage ceiling. These are single-run development measurements, not a cross-platform performance guarantee. Linux runtime and peak process memory were not measured in this environment.
