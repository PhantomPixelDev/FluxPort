# API controls

Configure **Settings → API controls**. Collection-specific controls live under the open file’s **API** view. These native forms require dashboard access and CSRF protection. They do not change API authentication or the demo’s public access.

## Request budgets

Rate limiting starts disabled. When enabled, each client IP has separate budgets for GET/HEAD and other API requests in a fixed window starting with its first request. POST queries use the other-request budget even though they do not edit data. Defaults are 120 reads and 30 other requests per 60 seconds. Budgets accept 1–1,000,000 requests and windows of 1–3,600 seconds.

A collection budget is additional to the global budget. Rejected requests consume neither budget. Invalid/authentication-failed API requests count when a budget applies. Browser preflight requests do not. Dashboard pages, file edits, static assets and health checks are outside these budgets.

Exhaustion returns HTTP 429 with JSON and Retry-After (seconds). X-RateLimit-Limit, X-RateLimit-Remaining and X-RateLimit-Reset (Unix seconds) describe the budget. When both scopes apply, X-Collection-RateLimit-* describes the collection budget.

Counters are bounded to 20,000 active IP/scope/method buckets per process. Capacity exhaustion returns 503 with Retry-After: 1. Settings persist; counters reset after restart and when their rate configuration changes. Multiple server instances do not share counters. For distributed enforcement use a reverse proxy or gateway.

## Proxies

Client addresses come from the socket by default. Behind a reverse proxy, configure only its actual IPs/CIDRs:

```text
FLUXPORT_TRUSTED_PROXIES=127.0.0.1,::1
```

The equivalent JSON setting is `"trusted_proxies": ["127.0.0.1", "::1"]`. Restart after changing it. X-Forwarded-For is ignored unless the socket peer is trusted, then traversed from right to left until an untrusted hop. The proxy must append/replace this header correctly. Avoid trusting arbitrary public clients or a broad network you do not control. This setting affects API rate-limit identity only.

## Pagination

Choose a default and maximum row count for collection data reads and POST queries. Global defaults of 0 retain existing endpoint defaults (GET: 50, POST query: 20); the maximum is 100. Collection values of 0 inherit. Collection caps cannot raise the global cap; effective defaults are clamped to that cap. An explicit limit above the cap returns 400. Metadata listing and dashboard pagination retain existing behavior.

## Browser origins

Leave allowed origins empty for same-origin use. Enter exact origins such as https://example.com or http://localhost:3000, one per line, without paths, credentials or trailing slashes. Up to 32 origins are supported; * must be used alone.

Approved origins receive CORS headers. Preflight allows GET, HEAD, POST, PUT, PATCH, DELETE and Accept, Authorization, Content-Type headers. Cookies/credentialed CORS are not supported. Bearer authentication still applies to actual requests. A disallowed actual request receives no CORS permission; CORS is browser policy, not authentication or a firewall.

