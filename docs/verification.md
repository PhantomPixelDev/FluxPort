# Verification

FluxPort 0.2.0, Go 1.26.1. Executed 2026-10-10 UTC / 2026-10-11 Europe/Berlin.

## Executed checks

- Windows amd64: `go test ./...`, `go test -race ./...` and `go vet ./...` passed.
- Linux amd64: all eleven package test binaries were built with CGO disabled and actually executed under the `podman-machine-default` WSL2 distribution. All passed. Linux race instrumentation was not run.
- Optimized Windows and Linux releases were built and executed; both report version 0.2.0.
- Fixture reproducibility and every format's declared differences passed. The six committed smoke files match their manifest byte sizes and SHA-256 checksums.
- Streaming parser checks cover Unicode, multiline CSV, nested values, numeric lexemes, malformed delimiters, oversized records and cancellation. Nested TOML exports retain the records wrapper.
- Metadata migration from version 1 to version 3 preserves collection IDs and existing settings. Restart marks unfinished jobs interrupted, verifies source contents and reuses valid index generations.
- Collection checks cover partial batch failure, demo idempotence, exclusions/restore, display names, large read-only behavior, worker limits, cancellation/retry, disk-reserve failures and changed sources during indexing. Tests include changes that preserve size and modification time, and concurrent pagination during exports/index replacement.
- Real HTTP upload test passes a 12 MB multipart request with a known Content-Length through a 1 MiB ordinary-request buffer limit. The 12,000 records are indexed successfully. Chunked multipart upload was also exercised during development.
- API checks cover authentication, job status/cancellation and indexing responses with HTTP 503 and Retry-After. Existing CRUD, transformations, schema and conversion tests pass.
- Opt-in large tests passed on Windows and Linux: approximately 100 MiB each of JSON arrays, CSV and NDJSON, and 1 GiB NDJSON. They verify exact record counts, last-page source order and bounded sampled Go heap. Sources and disposable indexes are removed by the test harness. See [performance.md](performance.md) for timings and memory methods.
- Chromium interactions passed for demo loading, a mixed valid/malformed batch, pushed job results, preview pagination/raw tabs, renaming, conversion downloads, removal, restoration and confirmed source deletion. A separate 55 MB upload was cancelled and retried through the UI; it became ready and read-only.
- The reusable [browser callback](../scripts/browser-checks.js) passed against the isolated preview server. It also checks desktop/mobile themes, page and confirmation-text overflow, and absence of idle refresh requests during a three-second observation. Progress updates use native SSE; no periodic polling is implemented.
- Final desktop and 390 px mobile screenshots were inspected. A mobile checkbox width issue found during inspection was corrected. Screenshots are local artifacts in output/playwright, outside embedded assets.

## Conversion fidelity follow-up: 0.2.1

On 2026-10-11, `go test ./... -count=1`, `go test -race ./... -count=1`, and `go vet ./...` passed on Windows. All 11 packages with tests were cross-compiled and executed successfully under Linux/WSL2. New coverage checks nested and multiline values, exact numbers, date text, strict JSON validation, ordered XML content, CSV delimiter/inference/safety options, streaming parity, persisted job options, and valid empty exports.

The converter Chromium callback passed, including uploads, round trips, conversion errors, retained previous results, header-only CSV and a zero-byte NDJSON download. Desktop and 390 px mobile screenshots were inspected without page overflow. See [conversion fidelity](conversion-fidelity.md) for the format boundaries and reproducible callback.

The revised parser also passed Windows and Linux/WSL2 indexing tests for 100 MiB JSON/CSV/NDJSON and 1 GiB NDJSON. Peak sampled Go heap was below 5 MiB on both platforms. Detailed follow-up measurements are in [performance](performance.md); the original 0.2.0 release measurements remain version-labelled.

## Reproduce

Converter inspection 0.2.2 was checked on 2026-10-11: the Windows full suite, race checks and vet passed; CLI, conversion and web package tests also executed successfully under Linux/WSL2. A 15-second two-worker fuzz run passed after 170,123 executions. Generated round-trip cases cover nested JSON/YAML/XML and representable CSV/TOML/NDJSON values. The new inspection Chromium callback passed reports, field-path differences, exact input trees, column rules, line errors, error clearing and uploaded-content previews. Desktop and 390 px mobile screenshots were inspected with no page overflow. See [converter inspection](converter-inspection.md) for controls and explicit preview/comparison limits.

Run `go test -race ./...` and `go vet ./...` from the repository root. Large tests are deliberately separate:

```powershell
$env:FLUXPORT_LARGE_TESTS = '1'
go test ./internal/collections -run TestLargeStreamingInputs -count=1 -v -timeout=10m
Remove-Item Env:FLUXPORT_LARGE_TESTS
```

The large tests need several GiB of free temporary disk space. They do not change existing source files.

For browser checks, start a separate FluxPort server with empty test data/state directories and no authentication. Install Playwright CLI as an optional developer tool and ensure its Chromium browser is available, then run:

```powershell
pwsh -NoProfile -File scripts/browser-checks.ps1 -BaseURL http://127.0.0.1:8080
```

The browser checks create, restore and permanently delete only their generated synthetic sources. Do not point them at a production instance. The callback can also be passed directly to Playwright CLI run-code with comment lines removed and remaining lines joined with spaces.

Use scripts/measure.ps1 for Windows release measurements and compile/run scripts/measure_linux.go on Linux. Raw reports and methods are linked in [performance.md](performance.md).

## Remaining environment limitation

Docker was unavailable locally during the original measurements. On 2026-10-11, the 0.2.2 image was built and deployed successfully with Compose on an Ubuntu VPS. Container health, HTTPS login, authenticated API access and a nested conversion round-trip passed. The Linux test suite also ran under WSL2, which differs from native Linux storage. See [hosted deployment](deployment.md).

The preview uses a separate directory under artifacts/preview. Production remains empty until files are imported or demo loading is explicitly selected. No Node.js or browser tooling is required to build or run FluxPort.


## 0.3.22 API controls

Adds optional process-local per-client request budgets globally and per collection, data pagination defaults/caps and CORS origin controls. Tests cover concurrent atomic budgets, bounded counters, window expiry, persisted settings, invalid configuration, proxy spoofing, CSRF, auth, preflight and both data query interfaces. Browser checks exercise native global and workspace forms, per-file isolation and responsive layouts in both themes. See [API controls](api-controls.md) for semantics and operational limits.
