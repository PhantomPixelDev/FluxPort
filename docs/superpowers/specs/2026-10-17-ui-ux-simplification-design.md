# FluxPort UI/UX Simplification — Design

Date: 2026-10-17 · Status: approved in chat by project owner

## Intent and why

The FluxPort web UI is precise but overwhelming. An owner using it says it is
"hard to understand" and it is "hard to work on files". Root causes confirmed by
audit (ui/templates, ui/static/js, internal/web):

- The default file screen exposes ~70 interactive targets, half hidden in
  popovers; there is no visual hierarchy telling a newcomer what to do first.
- "Edit" is reachable from 5 places with 3 models; nothing marks cells as
  editable; adding a record requires typing JSON.
- Downloading changes its shape at runtime (JS adds/removes format options,
  wording mutates, an auto-click fires).
- The words do not mean one thing: file/collection/dataset, Saved steps/Data
  pipelines/Saved recipes, Inspect/Summary/Column summaries, three "History"s,
  five "Apply"s.
- Drafts and views are auto-restored silently; status copy reads like
  developer jargon ("updating table…", "(Unix seconds)", raw job states).
- Result screens blur sample vs. full-file ("sample statistics", "up to 20
  rows") without a consistent pattern.

Goal: **simple but still powerful** — "simple spine, strong hand". Every power
lives in exactly one obvious place. Data engineers keep everything they have;
non-technical users get a guided path: open → see → change → download.

## Goals

1. One obvious way to make each change; the data grid is the center.
2. A calm, readable workspace: 4 tabs (Data · Clean · SQL · Summary), one ⋯
   menu for everything else, one primary button per view.
3. A predictable, labeled download form: File data / Rows / Format.
4. One name per concept everywhere the user reads: **file** (not
   collection/dataset), **steps** (not pipelines/recipes), **File history**,
   **Processing history**, **Summary**.
5. Honest state: no silent auto-restore; plain-language status; consistent
   sample-vs-full notices.
6. Edit affordances: hover/focus hints, tooltip guidance, plain add-record
   form built from the file's columns.

## Non-goals (this phase)

- No route URL changes, no API contract changes, no processing behavior changes.
- No server restructuring, no template engine swap, no CSS framework.
- Removing the dead template tree (`pages/detail|insights|quality|changes|
  download-review`, `partials/dataset-tabs|dataset-header`) is allowed only as
  deletion of files with no render references; no rewrite of live partials.
- Command palette and keyboard-shortcut sheet: deferred to a later phase.
- Moving File settings/Processing history into a right rail: deferred to a
  later phase. In this phase they remain panels at the bottom, reachable from
  the ⋯ menu.

## Anti-goals (what would make this a failure)

- Hiding any data-engineering capability so thoroughly it becomes undiscoverable
  (SQL, saved steps, contracts, quality checks, API publishing) — power must
  remain one click or one ⋯ away.
- Regressions on mobile: the ⋯ pattern and current responsive behavior must
  keep working; no fixed-width grids.
- Breaking `internal/web` tests or browser-based flow contracts that tests
  exercise (data attributes are hooks for JS and tests alike).

## Constraints

- Vanilla HTML/CSS/JS + Go html/template only; no new dependencies.
- Keep all `data-workspace-*`, `data-download-*`, `data-cleanup-*`,
  `data-file-editor`, `data-file-edit-status`, `data-prepared-*`,
  `data-draft-notice` hooks unless the paired JS change ships in the same task.
- Dark theme default stays; light theme keeps parity for every rule touched.
- The standalone converter page, SQL playground, API explorer, workflows guide
  change only in vocabulary, not layout.
- Go tests must pass: `go test ./...` (CGO on; GCC 15.2 available).

## Decisions made (owner approved in chat)

- Priority workstreams: editing clarity, workspace frame, download flow.
- Design direction: calm modern + spreadsheet-familiar table; one accent;
  typography-first; strong information hierarchy.
- Vocabulary: user-facing word is **file**; internal/API terms may keep
  technical names in API settings/documentation contexts.
- Owner's uncommitted doc edits (README.md, docs/dataset-workspace.md,
  docs/reference.md) are not touched by this project; a follow-up doc task in
  the main checkout will sync wording after merge.

## Left to the builder

- Exact phrasing of small consumer copy within the vocabulary rules, keeping
  tone plain and short.
- CSS specifics for hover/focus edit affordances consistent with tokens.
- Template reindentation (minified single-line files may be rewritten readable)
  provided rendering output is unchanged modulo the specified copy changes.
