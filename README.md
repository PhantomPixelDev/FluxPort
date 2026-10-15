# FluxPort — Self-hosted data explorer and converter

**Explore, clean and convert data files. Add SQL and REST APIs when you need them.**

FluxPort is a self-hosted workspace for CSV, JSON, Excel, Parquet, YAML, XML, NDJSON and TOML. Open a file, inspect its rows, remove duplicates or fix values, and download the result. Developers can reuse saved steps, query datasets with DuckDB, automate work from the CLI and publish REST endpoints.

[Try the demo](https://fluxport.ppxl.dev) · [Download 0.3.20](https://github.com/PhantomPixelDev/FluxPort/releases/tag/v0.3.20) · [Documentation](docs/reference.md) · [Report a bug](https://github.com/PhantomPixelDev/FluxPort/issues/new/choose)

[![Checks](https://github.com/PhantomPixelDev/FluxPort/actions/workflows/checks.yml/badge.svg?branch=main)](https://github.com/PhantomPixelDev/FluxPort/actions/workflows/checks.yml) · [MIT license](LICENSE)

![FluxPort Data workspace in light mode: full-width table, focused views and Convert / Download](docs/images/workspace.png)

## Work with files

Each file opens a focused workspace: **Data · Clean · SQL · Inspect**. Tables and editors use the main page, with **Convert / Download** and **File settings** always easy to find. On mobile, secondary actions stay in **⋯ More**.

1. **Open:** choose **Add files**, or **Try an example** to load small synthetic datasets.
2. **Explore:** search and filter rows, sort columns, resize or hide fields, and inspect complete values. Compact and Comfortable densities suit different screens.
3. **Edit:** double-click a cell (or focus it and press Enter), enter a value and press Enter to save. Escape cancels. Nested values open a larger JSON editor. **Undo**, **Redo** and **History** stay beside the file information.
4. **Clean:** trim spaces, remove duplicates, fill empty values or rename columns. **Apply change** saves to the same file and refreshes its table. Choose matching rows across every page or the entire file; unmatched rows remain untouched.
5. **Convert / Download:** choose rows and an output format. Downloads follow the current file and filters. **Original file** downloads its exact current bytes.

Small text files support direct editing without enabling REST writes. Saved versions survive restarts, with up to 20 versions or 64 MiB per file; Undo restores exact bytes. Saves may normalize the source formatting. Large files, Excel and Parquet remain read-only: cleanup prepares a working preview for download. SQL text, engine and view state restore in the same browser tab without executing SQL. See the [workspace walkthrough](docs/dataset-workspace.md).

| Make changes | Download the result |
| --- | --- |
| ![Clean data: cleanup controls above the same file table](docs/images/changes.png) | ![Download: one ready file after automatic preparation](docs/images/downloads.png) |

The online demo is a shared public workspace. Install your own instance for private data. Normal installations load examples only when requested.

## Get started

Download a verified executable and [checksums](https://github.com/PhantomPixelDev/FluxPort/releases/download/v0.3.20/SHA256SUMS):

| Platform | Download | Start |
| --- | --- | --- |
| Windows amd64 | [fluxport-windows-amd64.exe](https://github.com/PhantomPixelDev/FluxPort/releases/download/v0.3.20/fluxport-windows-amd64.exe) | `./fluxport-windows-amd64.exe serve` |
| Linux amd64 | [fluxport-linux-amd64](https://github.com/PhantomPixelDev/FluxPort/releases/download/v0.3.20/fluxport-linux-amd64) | `chmod +x fluxport-linux-amd64 && ./fluxport-linux-amd64 serve` |

Open **http://127.0.0.1:8080**. The executable includes the UI and DuckDB; it needs no Go compiler, Node.js runtime or external database. Linux binaries are built on Debian 12 and require the system C/C++ runtime (`libstdc++6`); release smoke checks run on Ubuntu 24.04. Windows binaries use the standard Windows runtime.

For Docker, clone this repository and run:

```sh
docker compose build --build-arg VERSION=0.3.20
docker compose up -d
```

Back up both `data/` and `.fluxport/`. Configure dashboard authentication and API access for your own deployment using the [configuration reference](docs/reference.md).

## Supported formats and fidelity

Dataset import/export and the standalone format converter have different capabilities:

| Format | Dataset import / export | Standalone converter | Mapping and limits |
| --- | --- | --- | --- |
| JSON | Yes / Yes | Yes | Numeric text stays precise; top-level arrays can stream. |
| CSV | Yes / Yes | Yes | Cells are text by default; optional converter inference. Nested values export as JSON text. |
| Excel `.xlsx` | Yes / Yes | No | Choose a worksheet; unique headers required and formulas rejected. Exports use text cells; null/missing/empty text become blank. Original download includes all sheets. |
| Parquet | Yes / Yes | No | Choose lossless JSON-record storage or typed analytical columns. Typed mode keeps CSV strings as text, uses exact decimals up to 38 digits, and stores nested/mixed/out-of-range values as text. |
| YAML | Yes / Yes | Yes | One document; unsupported tags and non-JSON values fail explicitly. |
| XML | Yes / Yes | Yes | General XML imports use a documented mapping; exports use a typed JSON envelope. |
| NDJSON | Yes / Yes | Yes | One JSON value per line; streaming import/export. |
| TOML | Yes / Yes | Yes | Dataset exports use a `records` wrapper. Nulls are unsupported; small conversions require an object root. |

Original downloads preserve the exact source bytes. Use Check format changes for a bounded review of actual source or saved-step rows before downloading. Format limitations and sample coverage are explicit. [Format fidelity guide](docs/conversion-fidelity.md) · [Excel and Parquet mappings](docs/platform.md)

## Build data workflows

- **Saved steps:** reusable cleanup, joins, calculations, output validation, revisions and scheduled runs through the full step editor.
- **SQL:** query, join, aggregate, filter and sort collection records. Automatic selection uses SQLite for small/simple work and DuckDB for suitable analytical queries; manual engine selection is available.
- **REST APIs:** pagination, search, schema, response transformations and optional size-bounded writes. Authentication is configurable; the public demo requires no bearer token.
- **Large datasets:** streaming JSON arrays, CSV and NDJSON, disk-backed indexes, background jobs and bounded record sizes. DuckDB analytical queries support disk spilling within configured limits.

Simple conversions remain native Go. Explorer and pipeline processing retain their established Go/SQLite paths; DuckDB does not replace SQLite metadata or every operation. [DuckDB guide](docs/duckdb.md) · [Explorer and SQL](docs/explorer-sql.md) · [Architecture](docs/architecture.md)

### CLI examples

```sh
fluxport convert input.json --to yaml --output output.yaml
fluxport convert input.csv --to json --delimiter auto --infer-types
fluxport convert input.json --to csv --spreadsheet-safe --output output.csv
fluxport import input.xlsx
fluxport export COLLECTION_ID --format parquet --output result.parquet
fluxport fixtures --suite smoke --out examples-local --seed 42
```

Input filenames precede flags. The standalone converter supports the six text formats; dataset commands also support Excel and Parquet. [CLI and processing examples](docs/platform.md)

### API example

```http
GET /api/collections/{collection-id}?page=1&limit=50
Authorization: Bearer YOUR_CONFIGURED_API_KEY
```

Open **API settings** (under **⋯ More** on mobile) to manage publication, or use the API explorer. See [endpoints, authentication and OpenAPI](docs/reference.md).

## Build from source

Requires Go 1.26+ and a compatible C/C++ compiler for the bundled DuckDB driver. Use GCC on Linux or MinGW-w64 on Windows.

```sh
git clone https://github.com/PhantomPixelDev/FluxPort.git
cd FluxPort
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o fluxport ./cmd/fluxport
./fluxport serve
```

On Windows use `go build -trimpath -o fluxport.exe ./cmd/fluxport` with GCC available on PATH. Node.js is optional for browser checks only; there is no frontend build step.

## Documentation and project status

- [User walkthrough](docs/dataset-workspace.md) and [configuration/API reference](docs/reference.md)
- [Large datasets and generated fixtures](docs/datasets.md)
- [Measured performance](docs/performance.md): reports identify their version, platform and memory metric; older measurements do not describe the current DuckDB build
- [Current capabilities and remaining roadmap](docs/platform.md)
- [Contributing](CONTRIBUTING.md), [security reporting](SECURITY.md), and [release notes](https://github.com/PhantomPixelDev/FluxPort/releases)

FluxPort uses Go + Fiber, embedded HTMX, vanilla CSS/JS and SQLite. Your source files stay accessible on disk. No AI services, Python runtime or external database are required.
