# Data workflow and collection conversion

The workspace connects Import, Inspect, Convert, and Download through a guided workflow page and collection-specific links. It is a navigation workflow, not a scheduled pipeline editor.

Previews search every source record, independently of API response transformations. Ordinary pages use the disk index; filtered scans keep the existing 25-second deadline. Search is explicit to avoid repeatedly scanning a large dataset while typing. Page sizes are 20, 50, or 100. Search and page size survive pagination, and the URL can be bookmarked. Up to 48 non-empty fields are offered; the first 12 are visible by default. Column choices are stored per collection in browser local storage. Raw JSON retains the existing 64 KiB page preview limit.

Converter input can be pasted, uploaded, or selected from existing collections. Collection names are searchable when there are more than 100. The source format follows the chosen collection. The converter snapshots the complete source, checks SHA-256 against the indexed generation, and uses the existing conversion path. This preserves object roots, nested values, CSV inference settings, and document conversion behavior. Changed or pending sources must be refreshed before conversion. Small snapshots are removed after the response. Large snapshots use cancellable background jobs and the existing 24-hour download/input cleanup.

Snapshots require additional disk space and read the source once. Conversion preparation remains subject to record, document, stream, and free-disk limits. Source files remain unchanged.

Validation: Go tests cover root preservation, same-size source changes with preserved timestamps, case-insensitive source search, API rule independence, filtered pagination, small collection conversion, multipart background conversion, and useful empty results. The Chromium check in `scripts/usability-checks.js` covers import → search → pagination → remembered columns → nested conversion, all four main screens at desktop/mobile widths and both themes, and page errors. Browser previews stay bounded to a single page.
