# Hosted deployment

FluxPort is hosted at https://fluxport.ppxl.dev. The public demo opens directly without a dashboard password or API bearer token. JSON, CSV, YAML, XML, NDJSON and TOML example datasets are preloaded for visitors. The portfolio overview and project page are at https://ppxl.dev and https://ppxl.dev/fluxport/.

The public source and Windows/Linux downloads are on GitHub:

- https://github.com/PhantomPixelDev/FluxPort
- https://github.com/PhantomPixelDev/FluxPort/releases/latest

Deployment uses the owner's portfolio hosting control repository. Its registry defines the domain and internal port; its FluxPort Compose file manages an internal-only service on the shared edge network, persistent data/state volumes, a 512 MiB memory limit and one CPU. The hosted configuration caps individual source files at 1 GiB and reserves 1 GiB free disk. These hosted limits do not change the application's local defaults.

Caddy terminates HTTPS, preserves the application's restrictive content-security and framing headers, and marks dashboard cookies Secure. Random credentials are stored in a private, ignored server-side environment file. No credentials, runtime databases, large fixtures or private keys are committed.

Verified on 2026-10-11:

- Docker image build and Compose startup on Ubuntu.
- Running, healthy FluxPort container.
- HTTPS health/login and portfolio routes return 200.
- The initial protected deployment validated login; the current public demo opens without authentication.
- Anonymous API requests and a nested JSON-to-YAML round-trip succeed.
- Strong CSP, DENY framing and Secure cookies are present.
- English/German portfolio card/detail page, screenshot loading and mobile layouts work in Chromium.

Public demo startup loads six small deterministic datasets once. Restarts do not duplicate examples or restore examples a user removed. Normal installations can explicitly load demo data. The operator must back up both source and state volumes.
