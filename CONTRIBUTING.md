# Contributing to FluxPort

Useful contributions include reproducible conversion bugs, clearer data workflows, format fixtures and improvements to bounded processing. Start with an issue describing the task and expected result; include a small synthetic example rather than private data.

## Development

Install Go 1.26+ and GCC/MinGW-w64. The normal build includes DuckDB.

```sh
go test ./...
go vet ./...
go build -o fluxport ./cmd/fluxport
```

Keep existing APIs and format mappings compatible. Use existing processing services for UI, CLI and API behavior. Preserve numeric precision and make target-format limitations explicit. Large fixtures, runtime state and generated downloads stay outside Git.

For browser checks, install the optional test tools with `npm ci` and `npx playwright install chromium`. Run an isolated local FluxPort instance, then `node scripts/browser-checks.cjs http://127.0.0.1:58903/collections`. The checks create and delete synthetic files; the runner rejects external hosts.

Use scoped component CSS, semantic HTML, native form fallbacks and both themes. Include meaningful tests for changed behavior and screenshots for visible UI changes. Pull requests should explain the user-visible result and validation performed.

## Releases

Version tags trigger Windows/Linux amd64 builds, Go checks, executable startup and DuckDB smoke tests. Verified binaries and SHA-256 checksums are published only after both package jobs pass. Keep release notes, README links, screenshots and the deployed version aligned.

Report vulnerabilities using [the security policy](SECURITY.md).
