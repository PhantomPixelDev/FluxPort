# Windows processing verification — 2026-10-11

Measured by the existing opt-in `TestLargeStreamingInputs` suite on the Windows development machine. Synthetic records contain about 1,000 bytes of padding. These are single-run results, not cross-platform performance promises.

| Input | Actual bytes | Records | Indexing | Throughput | Last-page latency | Peak Go heap |
|---|---:|---:|---:|---:|---:|---:|
| NDJSON | 104,858,560 | 102,210 | 11.44 s | 8.74 MiB/s | 5.19 ms | 3.87 MB |
| CSV | 104,857,989 | 104,136 | 10.01 s | 9.99 MiB/s | 4.11 ms | 3.96 MB |
| JSON array | 104,858,561 | 102,210 | 12.81 s | 7.81 MiB/s | 4.49 ms | 3.86 MB |

The 1 GiB NDJSON case **did not complete**. Indexing stopped at 268,390,400 free bytes, below the configured 268,435,456-byte reserve. The test therefore failed its ready-collection assertion. This run demonstrates the low-disk stop, not successful 1 GiB throughput or memory verification. The reserve was not disabled to make the test pass. Run the suite on a volume with enough room for the source, normalized SQLite index and temporary overhead.

Go heap measurements exclude DuckDB's native allocations, SQLite mappings, process RSS and operating-system caches. This suite exercises native ingestion/indexing and pagination; Parquet, joins and pipelines have separate correctness tests but no comparable large-scale performance report yet. [Linux/Docker verification](performance-linux-2026-10-11.md) is recorded separately. Concurrent API load and peak process RSS during processing remain outstanding.

The stripped standard Windows amd64 binary, including DuckDB and Excel support, measured **72,095,744 bytes (68.76 MiB)**. Five empty-workspace startup runs averaged **240.71 ms** to the first successful health check (189.01–317.44 ms). At one second after readiness, the average process working set was **23,980,442 bytes**, and private bytes were **56,509,235**. DuckDB was not initialized by these empty-workspace runs. The homepage plus its measured CSS/JS/icon assets transferred **41,474 bytes** with gzip. These measurements come from `scripts/measure.ps1`; they do not describe memory during analytical queries.

```sh
FLUXPORT_LARGE_TESTS=1 go test ./internal/collections -run '^TestLargeStreamingInputs$' -count=1 -v -timeout 14m
```

On PowerShell set `$env:FLUXPORT_LARGE_TESTS='1'` before running the same Go test command. Optionally set `FLUXPORT_BENCH_REPORT` to a JSON artifact path outside committed fixtures.
