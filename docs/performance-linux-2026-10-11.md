# Linux and Docker verification — 2026-10-11

Measured on the Linux VPS in an isolated Docker container with a 1,536 MiB memory limit, two CPUs and networking disabled. The host had approximately 3.9 GiB RAM, 4 GiB swap and 4.3 GiB free disk before the final test run. These are single-run measurements using the same synthetic inputs as [the Windows report](performance-windows-2026-10-11.md), not a controlled comparison between machines.

| Input | Actual bytes | Records | Indexing | Throughput | Last-page latency | Peak Go heap |
|---|---:|---:|---:|---:|---:|---:|
| NDJSON | 104,858,560 | 102,210 | 10.04 s | 9.96 MiB/s | 1.12 ms | 3.82 MB |
| CSV | 104,857,989 | 104,136 | 9.67 s | 10.34 MiB/s | 1.38 ms | 3.97 MB |
| JSON array | 104,858,561 | 102,210 | 12.11 s | 8.26 MiB/s | 0.93 ms | 3.83 MB |
| NDJSON | 1,073,742,046 | 1,045,577 | 126.92 s | 8.07 MiB/s | 1.45 ms | 3.98 MB |

All three 100 MiB cases and the 1 GiB NDJSON case passed pagination and the bounded Go-heap assertion. The configured 256 MiB free-disk reserve remained enabled. Go heap excludes native DuckDB allocations, SQLite mappings, process RSS and operating-system caches. This suite measures native ingestion/indexing and pagination, not large analytical joins or Parquet export. The complete large-input suite took 163.13 seconds.

The production Dockerfile built successfully, and its runtime image executed `fluxport version` with networking disabled. The runtime installs `libstdc++6`, required by the Linux DuckDB driver, and CA certificates. The complete Go test suite also passed on Linux using the latest source mounted into the builder image. Verification used separate temporary directories and image tags; it did not replace the deployed application.

Large analytical engine comparisons, concurrent API-load measurements and processing peak RSS remain outstanding. Windows startup, idle memory, binary size and frontend transfer measurements are recorded separately; equivalent Linux measurements have not yet been collected.
