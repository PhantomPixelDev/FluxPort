# Release measurements

## Conversion fidelity follow-up: 0.2.1

Measured on 2026-10-11 using the same opt-in streaming test and approximately 1 KiB records. Counts and final source-order pagination passed in every case. Peak sampled Go heap stayed below 5 MiB on both platforms; this measures Go heap, not total process memory. Linux tests actually ran under WSL2 using the Windows-mounted workspace.

| Input | Records | Windows index / page | Linux index / page |
| --- | ---: | ---: | ---: |
| 100 MiB NDJSON | 102,210 | 10.65 s / 5.03 ms | 5.30 s / 2.08 ms |
| 100 MiB CSV | 104,136 | 4.87 s / 5.80 ms | 3.09 s / 1.21 ms |
| 100 MiB JSON array | 102,210 | 7.88 s / 7.19 ms | 4.71 s / 3.65 ms |
| 1 GiB NDJSON | 1,045,577 | 41.54 s / 10.04 ms | 40.96 s / 2.09 ms |

Raw follow-up reports: [Windows](fidelity-large-windows.json), [Linux](fidelity-large-linux.json). Optimized 0.2.1 executables are 19,369,472 bytes on Windows and 19,071,138 bytes on Linux. Startup, idle process memory and transfer measurements below belong to 0.2.0 and were not remeasured in this follow-up.

## Original release: 0.2.0

FluxPort 0.2.0, Go 1.26.1, measured on 2026-10-10. Optimized amd64 binaries use -trimpath and -ldflags="-s -w -X main.version=0.2.0". These are local observations, not capacity or latency guarantees.

| Metric | Windows | Linux under WSL2 |
| --- | ---: | ---: |
| Executable | 18.40 MiB | 18.12 MiB |
| Median launch to HTTP health | 105.76 ms | 204.43 ms |
| Median idle working set / RSS | 18.00 MiB | 15.50 MiB |
| Initial overview and all assets | 28526 bytes | 28529 bytes |

Windows idle private allocation median: 48.89 MiB. Working set, private allocation, RSS and Go heap measure different things.

## Large streaming inputs

| Platform | Input | Records | Index seconds | MiB/s | Last-page ms | Sampled peak Go heap MiB |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| windows | 100 MiB ndjson | 102210 | 10.29 | 9.72 | 6.68 | 4.71 |
| windows | 100 MiB csv | 104136 | 5.06 | 19.77 | 7.78 | 3.84 |
| windows | 100 MiB json | 102210 | 12.20 | 8.20 | 8.28 | 4.39 |
| windows | 1 GiB ndjson | 1045577 | 102.84 | 9.96 | 8.05 | 4.24 |
| linux | 100 MiB ndjson | 102210 | 10.80 | 9.26 | 2.97 | 3.71 |
| linux | 100 MiB csv | 104136 | 6.51 | 15.36 | 2.85 | 3.90 |
| linux | 100 MiB json | 102210 | 12.61 | 7.93 | 2.64 | 3.69 |
| linux | 1 GiB ndjson | 1045577 | 104.23 | 9.82 | 2.69 | 3.79 |

Index measurements include parsing, SQLite writes and source checksum verification. Input generation finishes before timing indexing. All cases checked exact counts and the final source-order ordinal; sampled Go heap growth stayed below the test threshold of 96 MiB. Each case is a single-file workload with approximately 1 KiB payloads. Large fixtures and indexes were temporary and were removed after each case.

The external process sampler observed Windows peak working set 30.16 MiB and Linux peak RSS 24.99 MiB. These counters include the test harness and fixture generation. Windows had only 1 observed sample; Linux had 61. They are observed process high-water counters, not a comprehensive application memory profile. Go heap samples were collected every 10 ms during indexing.

## Method and limits

Five fresh server processes per platform, empty source folders and fresh metadata databases. Health checks retry every 10 ms. Idle counters are read one second after readiness, before dashboard traffic. Windows uses scripts/measure.ps1 and Windows process counters. Linux uses scripts/measure_linux.go and /proc VmRSS/VmHWM. Linux ran on Linux 6.6.87.2-microsoft-standard-WSL2 in the podman-machine-default WSL2 distribution, using the Windows-mounted workspace; it was actually executed, not inferred from a cross-build. Filesystem and virtualization affect these results.

Startup with existing collections also verifies source hashes in background; the empty-folder startup result does not measure reconciliation of 1 GiB sources. Filtered and transformed queries scan indexed records with a 25-second request deadline and can take substantially longer than ordinary pagination.

Frontend payloads use Accept-Encoding: gzip and exclude headers, TCP/TLS overhead and browser caching. The logo uses identity encoding. No external fonts or JavaScript CDN are required.

Docker CLI was unavailable. Container image builds and Compose runtime remain unverified.

Raw reports: [Windows](performance-windows.json), [Linux](performance-linux.json), [large Windows](large-windows.json), [large Linux](large-linux.json), [process sampler](large-process-memory.json).
