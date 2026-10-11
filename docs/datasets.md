# Dataset generation and verification

The fixtures CLI incrementally writes JSON, CSV, YAML, XML, NDJSON and TOML with deterministic records and fixed timestamps. Users, products, orders and events contain Unicode names, synthetic addresses, SKUs, stock, order statuses, references and event/session fields. Each suite uses all six formats and all four profiles; profile selection is recorded in the manifest. CSV uses a fixed field union with text cells and empty cells for absent/null values. TOML uses an explicit records wrapper.

Use a fresh output directory. Smoke has 100 records per file and is committed under examples/fixtures. Integration has 10,000. Large writes both 100,000 and one million per format. Stress requires explicit rows or bytes, applied to each format. Large files belong in ignored artifacts and are never embedded. YAML/TOML/XML large fixtures intentionally exceed default document import limits and exercise rejection.

```sh
./fluxport fixtures --suite smoke --out artifacts/fixtures/smoke --seed 42
./fluxport fixtures --suite integration --out artifacts/fixtures/integration --seed 42
./fluxport fixtures --suite large --out artifacts/fixtures/large --seed 42
./fluxport fixtures --suite stress --out artifacts/fixtures/stress --seed 42 --bytes 1073741824
```

The stress example produces over 6 GiB across six files before indexes. Byte targets can exceed the target by one buffer/record plus format framing. Existing datasets are never overwritten. Manifests record suite, seed, profile, record count, bytes, SHA-256 and representation differences. TOML counts describe records inside its wrapper; unchanged object-root import semantics expose that document as one collection row.

Smoke also includes Unicode, multiline CSV, missing fields/nulls, nested objects, large integers, malformed sources and incompatible conversion cases. Load demo data explicitly adds six small examples once per instance. It never populates normal installations automatically or restores examples a user removed/deleted.

Automated tests cover byte reproducibility, format behavior, nested TOML, Unicode/integer precision, malformed/oversized records, partial batches, streamed uploads beyond request buffering, exclusions/restoration, disk failures, source changes during indexing, worker bounds, cancellation, restart/index recovery, exports, authenticated jobs/503 and existing CRUD/transform semantics.

Opt in to actual large-file verification:

```sh
FLUXPORT_LARGE_TESTS=1 FLUXPORT_BENCH_REPORT=artifacts/large.json go test ./internal/collections -run TestLargeStreamingInputs -count=1 -v -timeout=10m
```

In PowerShell set the environment variables with `$env:NAME = 'value'` first. The harness creates 100 MiB NDJSON, CSV and JSON-array sources plus a 1 GiB NDJSON source in test-owned temporary directories. It verifies exact counts and the final paginated record, samples Go heap every 10 ms, asserts less than 96 MiB heap growth and removes generated sources/indexes after each case. Reports measure indexing/pagination for these distributions, rather than every possible format, record shape or concurrency level.

See performance.md for actual Windows/Linux results and verification.md for browser checks and environment limitations.
