# Architecture

FluxPort retains one Go/Fiber process, embedded Go HTML templates, local HTMX, vanilla CSS/JS, SQLite metadata and fsnotify. No SPA, frontend bundler, resident dataset cache or per-record goroutines are introduced.

## Data path

Authenticated multipart file parts stream to hidden temporary files with disk and format limits. Completed sources are renamed into the watched folder. Independent files enter a persisted worker queue; a partial batch failure does not roll back other files. Display names are independent of physical filenames.

CSV, NDJSON and top-level JSON arrays emit one bounded record at a time. Other document shapes remain size bounded. Each job writes source-order ordinals and normalized JSON into a disposable SQLite index, using a bounded page cache and transaction batches. Cancellation, disk reserve, source size/mtime, a content checksum and the current collection job are checked before publication. Completed files are fsynced/renamed before their metadata generation is selected.

Ordinary pages seek indexed positions and read a bounded page. Filter/search/transform requests scan with bounded memory and a deadline. Metadata caches observed schema and processing status. Generation cleanup coordinates with active readers of that collection; different collections remain independent. Source files remain authoritative, and missing indexes can be rebuilt.

CRUD retains bounded atomic source replacement. Large sources expose effective read-only status. Detected source changes invalidate new reads until replacement indexes are ready. External applications should preserve normal mtime behavior and coordinate with app CRUD; size/mtime detection does not lock external applications transactionally.

## Jobs and events

`prepare_result` jobs write private SQLite indexes and JSON manifests in the state directory’s `results/`, outside the watched source root. Publication uses fsync/rename; ownership, dependency generations and expiry are checked server-side. An internal immutable index reference lets original and cleaned data share bound query/rendering paths, holding a lease throughout reads and exports. Recipes may store `input_query`: page/limit are discarded while filters and sort are retained. Output browsing queries remain separate.

Preparations are keyed by dependency generations, captured query, steps and validation rules; equivalent work is reused. UI request/result identities prevent old completions replacing newer drafts. Export dependencies stay staged until preparation succeeds, using no worker while waiting. Job payloads and result manifests persist descriptors without a metadata migration. Restart interrupts incomplete jobs and removes abandoned partials; ready results survive unchanged-source verification. Results expire after 24 hours, and hourly/startup/on-preparation cleanup removes expired unleased indexes.

Migration version 3 preserves existing IDs and configuration and adds display names, exclusions, stamps, schemas, generation pointers and jobs. A staged phase prevents workers claiming indexes before collection linkage. Default workers are at most two, limited by CPUs. Handlers report throttled progress, honor cancellation and contain panics as failures. Restart marks unfinished jobs interrupted; startup reconciles affected sources.

Authenticated native SSE streams carry job IDs, resources and states. The browser debounces events and requests HTMX fragments. Preview page/tab state is retained, and only the affected detail refreshes. A 20-second comment heartbeat maintains connections. No periodic browser polling runs.

Watcher events debounce affected files and directory subtrees. Full scans are reserved for startup, explicit reconciliation and watcher overflow/event-storm recovery. Collection-specific coordination replaces the global file-operation mutex. Hidden conversion inputs are excluded from discovery. Temporary downloads and private inputs have 24-hour retention, with cleanup on startup and conversion creation.

## Interface and bounds

Overview focuses on totals, collections and synchronization issues. Collections has expandable multi-file input/drop support, activity, explicit demo loading and exclusions. Details has table/raw/schema/configuration tabs, names, refresh/retry and separately confirmed source deletion. Remove persists exclusions while preserving sources.

## Conversion fidelity

Values survive every format without HTML escaping: JSON, NDJSON and the XML envelope write `<`, `>`, `&` as themselves rather than `\u003c` and friends. XML character data escapes only what the grammar requires, so the envelope stays readable.

JSON numbers keep their source lexeme. Integers within signed 64 bits and finite floats round-trip exactly; YAML and TOML have no wider types, so a lexeme outside those bounds is written as an explicitly quoted string that reads back as the same characters instead of emitting a document the parser rejects.

Import trims a leading UTF-8 byte-order mark from every format, so a BOM added by a Windows export does not leak into the first CSV header or break JSON. Empty input reports one message instead of the underlying decoder error. XML keeps the prefix each document declared, and shows an undeclared prefix verbatim, so `<a:x>` and `<b:x>` never collapse into one key.

Streamed and buffered conversion of the same records produce identical bytes: the same indentation, sequence-item layout, escaping rules and empty-stream results. A byte-order mark, whitespace-only input and every format above are covered by tests in `internal/conversion`.

Forms, buffered documents, records, schema fields, page sizes, preview display, processing workers, SSE subscribers and disk reserve have explicit bounds. Streaming removes the whole-file ceiling by default but does not promise unlimited capacity. See README for defaults.

Startup validates cached generations by SHA-256 before serving them; unchanged sources reuse their indexes without reparsing. Building an index hashes the bytes being parsed and verifies the source again before publication. Detected write events mark the source pending immediately, including same-size writes that preserve timestamps. External filesystems that miss events require explicit reconciliation/restart; reads cannot transactionally lock unrelated external writers.
