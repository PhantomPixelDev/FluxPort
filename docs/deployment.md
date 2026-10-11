# Hosted deployment

FluxPort is hosted at https://fluxport.ppxl.dev. The public demo opens directly without a dashboard password or API bearer token. JSON, CSV, YAML, XML, NDJSON and TOML example datasets are preloaded for visitors. The portfolio overview and project page are at https://ppxl.dev and https://ppxl.dev/fluxport/.

The public source and Windows/Linux downloads are on GitHub:

- https://github.com/PhantomPixelDev/FluxPort
- https://github.com/PhantomPixelDev/FluxPort/releases/latest

Deployment uses the owner's portfolio hosting control repository. Its registry defines the domain and internal port; its FluxPort Compose file manages an internal-only service on the shared edge network, persistent data/state volumes, a 512 MiB memory limit and one CPU. The hosted configuration uses one processing worker, a 128 MiB DuckDB memory budget, a 1 GiB DuckDB temporary-data limit and a 30-second query timeout. It caps individual source files at 1 GiB and reserves 1 GiB free disk. These hosted limits do not change the application's local defaults.

Caddy terminates HTTPS, preserves the application's restrictive content-security and framing headers, and marks dashboard cookies Secure. Random credentials are stored in a private, ignored server-side environment file. No credentials, runtime databases, large fixtures or private keys are committed.

Verified on 2026-10-11:

- FluxPort 0.3.0 deployed from commit `72da358`, with metadata migration through version 9.
- Docker image build and Compose startup on Ubuntu.
- Running, healthy FluxPort container.
- Container health checks use `fluxport healthcheck --url http://127.0.0.1:4170/healthz`; no external HTTP utility is required.
- HTTPS health/login and portfolio routes return 200.
- The initial protected deployment validated login; the current public demo opens without authentication.
- Anonymous API requests and a nested JSON-to-YAML round-trip succeed.
- Strong CSP, DENY framing and Secure cookies are present.
- English/German portfolio card/detail page, screenshot loading and mobile layouts work in Chromium.
- Overview, Collections, Pipelines, SQL and collection quality pages return 200 after the update.
- A live DuckDB count query returns the expected 100 demo records; profiling reports complete coverage of the same collection. All six existing collections remain ready.

Before this update, the operator stopped FluxPort briefly and archived both persistent volumes to `~/backups/fluxport/20261011-72da358/volumes.tgz`. The previous image is retained under `fluxport:rollback-20261011-72da358`. Rolling back a metadata migration requires restoring a compatible metadata backup, not merely changing the image.

Public demo startup loads six small deterministic datasets once. Restarts do not duplicate examples or restore examples a user removed. Normal installations can explicitly load demo data. The operator must back up both source and state volumes.
