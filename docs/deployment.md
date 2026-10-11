# Hosted deployment

FluxPort 0.2.2 is hosted at https://fluxport.ppxl.dev. The workspace requires a dashboard password; API endpoints require a separate bearer key. The portfolio overview and project page are at https://ppxl.dev and https://ppxl.dev/fluxport/.

The public source and Windows/Linux downloads are on GitHub:

- https://github.com/PhantomPixelDev/FluxPort
- https://github.com/PhantomPixelDev/FluxPort/releases/tag/v0.2.2

Deployment uses the owner's portfolio hosting control repository. Its registry defines the domain and internal port; its FluxPort Compose file manages an internal-only service on the shared edge network, persistent data/state volumes, a 512 MiB memory limit and one CPU. The hosted configuration caps individual source files at 1 GiB and reserves 1 GiB free disk. These hosted limits do not change the application's local defaults.

Caddy terminates HTTPS, preserves the application's restrictive content-security and framing headers, and marks dashboard cookies Secure. Random credentials are stored in a private, ignored server-side environment file. No credentials, runtime databases, large fixtures or private keys are committed.

Verified on 2026-10-11:

- Docker image build and Compose startup on Ubuntu.
- Running, healthy FluxPort container.
- HTTPS health/login and portfolio routes return 200.
- Dashboard login succeeds; unauthenticated dashboard traffic redirects to login.
- Authenticated API requests and a nested JSON-to-YAML round-trip succeed.
- Strong CSP, DENY framing and Secure cookies are present.
- English/German portfolio card/detail page, screenshot loading and mobile layouts work in Chromium.

The hosted workspace starts empty. Users running their own installation can explicitly load demo data. The operator must back up both source and state volumes.
