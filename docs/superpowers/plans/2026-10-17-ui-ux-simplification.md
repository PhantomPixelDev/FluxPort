# FluxPort UI/UX Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the FluxPort file workspace calm and self-explanatory — one way to edit, one download flow, one vocabulary — without removing any data-engineering capability.

**Architecture:** Server-rendered Go html/template pages (per-file shell `pages/workspace.html` + fragment partials), vanilla CSS on design tokens, small vanilla JS. All changes are front-of-house: template copy/structure, CSS tokens, and JS strings/hooks that pair with them. No route, handler-contract, or processing changes.

**Tech Stack:** Go 1.26 templates, CSS custom properties (`ui/static/css/tokens.css`), vanilla JS, existing `internal/web` httptest-covered render paths.

**Spec:** `docs/superpowers/specs/2026-10-17-ui-ux-simplification-design.md` (read alongside; steps argue from it)

## Global Constraints

- No URL/route changes; no Go API/processing changes (only template-func additions are allowed, see Task 5).
- Preserve every `data-*` hook JS/tests use unless the paired JS edit ships in the same commit: `data-workspace-target`, `data-workspace-data`, `data-download-source|steps|row-scope|note|submit|count|pending|scope`, `data-cleanup-action|scope|field|for|form`, `data-editor-open`, `data-file-editor`, `data-file-edit-status`, `data-file-undo|redo`, `data-file-history-action`, `data-prepared-result|state|browse-prepared|refresh-prepared`, `data-draft-notice`, `data-collection-download`, `data-state`, `data-recent-files`, `data-workspace-theme` (+ `-desktop`).
- Dark theme default; every CSS rule added must also work in `[data-theme=light]`.
- User-visible word rules: "collection(s)" → "file(s)", "dataset" → "file" (exception: keep in API settings keys/IDs and the SQL/API docs strings where a Go test asserts, then update the test only if purely copy), "steps" as the single name for saved recipes/pipelines ("Saved steps"), "File history" (edits), "Processing history" (jobs), "Summary" (insights view).
- Templates are minified today; rewrite them readable (1 attr per line where long) — rendering output must stay valid; no blank-text-node regressions in `{{if}}` chains.
- Every task ends with `go test ./internal/web/...` green; Task 8 runs the full `go test ./...`.
- Commits: one per task, subject `ux: <topic>`; never stage the owner's modified `README.md`, `docs/dataset-workspace.md`, `docs/reference.md`.

## Review Focus

1. **JS-attr decoupling:** a renamed/removed `data-*` hook or class (`.workspace-more`, `.workspace-primary-views`, `.toolbar-popover`) silently kills a JS feature. Test: after Navigation task, run the browser-checks script sections for workspace tool switching (`scripts/browser-checks.cjs`) or at minimum grep JS for every removed selector.
2. **Conditional option drift:** the download "Changes preview" option must exist exactly when `{{if or .DownloadDraft .ResultID}}`; wrong state = wrong default for background-prepared results. Test: `internal/web/download_flow_test.go` variants (`download_flow` cases with/without ResultID).
3. **honest sample vs full** must remain stated wherever data shown is partial (cleanup preview, insights, download review). Test: grep after tasks — every `data-workspace-fragment` partial that renders ≤20 rows keeps an explicit notice; `cleanup_preview_test.go` strings stay matched (update copy side of test together).
4. **Light theme contrast** for new hover/focus cell hints and the ⋯ menu. Test: manual both themes; no dedicated test — verify with tokens vars only.
5. **Record dialog reachable for non-editable files:** inline edit disabled (`FileEditable=false`) must not remove the row-number inspector. Test: `workbench_test.go` / `sheet_display_test.go` render with read-only collection.

---

### Task 1: Vocabulary sweep (copy only, no layout)

**Files:**
- Modify: `ui/templates/layouts/base.html`, `ui/templates/pages/overview.html`, `ui/templates/pages/collections.html`, `ui/templates/partials/collections.html`, `ui/templates/pages/pipelines.html`, `ui/templates/pages/saved-steps.html`, `ui/templates/partials/workspace-advanced.html`, `ui/templates/partials/workspace-clean.html`, `ui/templates/partials/settings.html` section labels as found, `ui/templates/pages/settings.html`
- Test: existing `internal/web` suite (copy assertions updated only when stale)

**Interfaces:**
- Produces: vocabulary rules above hold repo-wide in `ui/templates` (grep list below).

- [ ] **Step 1: Grep the copy inventory**

Run: `grep -n -i "collection\|dataset\|recipe\|pipeline" ui/templates | grep -v "={{\|value=\"\|action=\"\|href=\"\|name=\"\|data-\|{{.*}}"` (template code refs stay; user-visible copy changes).
Record the exact user-visible phrase list.

- [ ] **Step 2: Apply replacements (exact pairs)**

- `base.html:31` nav stays "Files/Saved steps/Settings"; sidebar "Advanced tools" summary → keep label but inner item "SQL queries" → "SQL", "Quick conversion" → "Convert files", "System overview" → "System status".
- `overview.html:1`: `Your collections at a glance.` → `Your files at a glance.`
- `overview.html:2`: `COLLECTIONS` → `FILES`
- `partials/collections.html`: `No matching collections` → `No matching files` (and any `…collections…` empty-state copy)
- `pipelines.html:1` h1 `Data pipelines` → `Saved steps`; page `<p>` subtitle → `Reuse the steps you saved from a file.`
- `workspace-advanced.html`: `Saved recipes` → `Saved steps`; section copy re `recipes` → `steps`
- `workspace-clean.html:6,17`: `Reuse steps from another dataset` → `Reuse steps from another file`; join-builder labels `Find a dataset`→`Find a file`, `Other dataset`→`Other file`, `dataset names`→`file names`
- `download-chooser.html:5`: `Entire dataset` → `All rows` (keep `Entire file` wording if present in clean form consistent: matching→`Filtered rows`, entire→`All rows`)
- Settings/copy stragglers: `settings.html` `Watched directory` keep; `Automatically synchronize collections`, if present → `Automatically pick up new files`
- Keep untouched: `?tab=configuration` API settings labels that reference collections only together with API docs terms (`Collection ID` allowed); `<title>` suffix; CSS class names; `Catalog` if any.

- [ ] **Step 3: Test**

Run: `go test ./internal/web/...`
Expected: PASS; if a copy assertion fails, update the test string in the same commit (allowed: pure copy), never the assertion's semantic (status, state, attrs).

- [ ] **Step 4: Commit**

```bash
git add ui/templates internal/web
git commit -m "ux: one vocabulary — files, steps, history"
```

### Task 2: Workspace toolbar — four tabs and one ⋯ menu

**Files:**
- Modify: `ui/templates/partials/workspace-navigation.html`, `ui/templates/pages/workspace.html`
- Test: `internal/web/workspace_views_test.go`, `file_workspace_test.go`

**Interfaces:**
- Consumes: existing JS hooks `data-workspace-target`, groups `inspect`/`file`, `.workspace-more`.
- Produces: nav that renders exactly 4 primary links (Data, Clean, SQL, Summary) + one `details.workspace-more` (menu label `File`), all targets conserved.

- [ ] **Step 1: Rewrite `workspace-navigation.html`**

Primary list: `Data` (→ `/collections/{id}`, `data-workspace-data`), `Clean` (unchanged href/query logic, target `clean`), `SQL` (target `sql`), `Summary` (was Inspect; target `insights`, rename both label and `workspace.html:8` heading `Inspect data` → `Summary`). Keep `aria-label`s updated (`Inspection views` → `Summary views`).
Right side: a single `<details class="workspace-more">` with `<summary class="button" aria-label="More file options">⋯</summary>` containing, in order: `File history` (target `revisions`, href as existing subnav), `File settings` (→ `#file-settings`), `API settings` (→ `?tab=configuration`), `Saved views` (→ `#saved-views`), `Processing history` (→ `#activity`). Remove the standalone toolbar "File settings" link (menu-only now). Keep the two `workspace-subnav` navs byte-equivalent in attrs.

- [ ] **Step 2: Check JS coupling**

Run: `grep -n "workspace-primary-views\|workspace-more\|workspace-file-action\|data-workspace-target=\"settings\"" ui/static/js/*.js ui/templates/partials/*.html`
Adjust only selector-specific JS lines if a class was removed (e.g. the ⋯ position logic in `file-workspace.js` that moves `#theme-toggle`/Sign out into `.workspace-extra-tools` must keep working — keep that div present).

- [ ] **Step 3: Test**

Run: `go test ./internal/web/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add ui/templates ui/static/js
git commit -m "ux: file toolbar — data, clean, sql, summary"
```

### Task 3: Clean view — make the outcome obvious

**Files:**
- Modify: `ui/templates/partials/workspace-clean.html`, `ui/templates/partials/cleanup-result.html`
- Test: `internal/web/cleanup_preview_test.go`, `dataset_workspace_test.go`

**Interfaces:**
- Consumes: form action names `apply|undo|reset|preview|save|save_change|add_join|search`, `data-cleanup-*` attrs unchanged.
- Produces: identical form semantics, new heading copy; `stepName`/`stepJSON` funcs untouched.

- [ ] **Step 1: Sentence at top of the form panel**

First line inside `cleanup-form` section: `<h2>Change your file</h2><p class="muted">Pick a change, preview it, then apply it to the file. The original file is never modified by preview.</p>` (replaces nothing — insert above `Rows to clean` row; adjust CSS spacing class `cleanup-add` heading styles).

- [ ] **Step 2: Scope wording**

`Rows to clean` label → `Rows`; options: `Matching rows`→`Filtered rows (search box in Data)`, `Entire file` → `All rows`. Keep option values.

- [ ] **Step 3: Apply button prominence**

In `Steps to apply` panel: keep order but the primary button text `Apply these steps to file` → `Apply to file`; and when `not .Pipeline.Definition.Steps`, show a muted `<p>No steps yet — build one above.</p>` instead of `<li class="muted empty-changes"></li>`.

- [ ] **Step 4: Sample honesty in cleanup-result**

`cleanup-result.html` sample banner: `Quick sample · up to 20 rows. Preparing the full result in the background.` → `Previewing the first 20 rows. The full result is prepared in the background — the applied change is exact for every row.` Keep `role=status`.

- [ ] **Step 5: Test**

Run: `go test ./internal/web/...`
Expected: PASS (update stale copy assertions).

- [ ] **Step 6: Commit**

```bash
git add ui/templates
git commit -m "ux: clean view — name the outcome, honest preview"
```

### Task 4: Download flow — one predictable form

**Files:**
- Modify: `ui/templates/partials/download-chooser.html`, `ui/templates/partials/download-job.html`, `ui/static/js/file-workspace.js` (strings only + option-permutation guard)
- Test: `internal/web/download_flow_test.go`, `downloads_test.go`

**Interfaces:**
- Consumes: `data-download-*` hooks unchanged; JS read of `select[data-download-source]` change event to rewrite format options.
- Produces: format `<select>` options rendered statically complete (enabled/disabled server-side); JS only enforces business rule, never adds/removes/rewrites option text at runtime.

- [ ] **Step 1: Labels**

`Data` label → `File data`; option texts: `original` → `Current file (as stored)`, `current` → `Changes preview`, `Saved cleanup: NAME` → `Saved steps: NAME`. Rows label → `Rows`, options `Filtered rows` / `All rows` (mirror Task 3 wording). Submit button text unaffected (`Download`).

- [ ] **Step 2: Review within reach**

Move the `Check format changes` button (currently inside `details.download-notes`) to sit right after the format note paragraph, before the details block; keep `formaction="/collections/{id}/export-check"`, `value="review"`; the details block keeps only format-explanation copy, summary text → `Format details`.

- [ ] **Step 3: JS parity in `file-workspace.js`**

Find the block that mutates `#collection-download-format` options on `change` of `data-download-source` (search `dataset` in the JS). Restrict it to: toggling `disabled` on existing `<option>`s and selecting the previously used format from `localStorage['fluxport-export-format-<id>']` if still present. No text rewriting, no option insertion/removal. Status strings: `Creating your file…` kept; add hint span text `Your download will start automatically when it is ready.` next to it (append into `#collection-download-status` when job queued; plain template fallback in `download-job.html` `<small>` line).

- [ ] **Step 4: Test**

Run: `go test ./internal/web/...` then grep: `grep -n "Changes preview\|Current file" ui/static/js ui/templates` — no runtime-construction of option labels from JS.

- [ ] **Step 5: Commit**

```bash
git add ui/templates ui/static/js
git commit -m "ux: downloads — static stable form, plain status"
```

### Task 5: Editing affordances — visible editability, plain add-record

**Files:**
- Modify: `ui/static/css/components.css` (append section), `ui/static/css/workspace.css` (only if a selector fight shows), `ui/static/js/workbench.js`, `ui/templates/partials/editor-panel.html`, Go side: template funcmap registration file (locate via `grep -rn "template.New\|Funcs(" internal/web`)
- Test: `internal/web/file_editing_test.go`, `workbench_test.go`

**Interfaces:**
- Consumes: `.Fields []collections.Field{Name,Types}` present in the page struct passed to `partials/detail` (server.go:90,558) — the Edit panel already renders inside that context.
- Produces: new template funcs (add to the same funcmap): `fieldType(t string) string` returning `checkbox` if t contains `boolean`, `number` if t contains `number`, else `text`; template change only uses these.

- [ ] **Step 1: CSS hints (append, both themes)**

Append to `components.css`: `[data-cell-editable]` hover cursor `text` + subtle inset outline using `--accent-bg`; `.workspace-tool:focus-visible` ring using `--accent`; a `.edit-hint` muted small-text style; ensure identical behavior in light via existing token vars only. Mark editable cells from JS (Step 3) so CSS keys off `data-cell-editable`.

- [ ] **Step 2: Add-record plain form in `editor-panel.html`**

Inside the add-record block (`Action` select `add` path, placeholder `{"name":"New row"}`): render `<fieldset data-add-fields>` with one labeled input per `.Fields` (label = `.Name`, `type=fieldType .Types`, default `"jsonb"` wrapped inputs kept for object/array types as JSON `<textarea>`), plus an auto-join on submit building the existing form field (`payload`/`values` name — match the current handler's input name exactly; locate it in `internal/web/file_commands.go`). Keep the raw JSON `<textarea>` under `<details><summary>Advanced: raw JSON</summary>` for engineers.

- [ ] **Step 3: workbench.js — mark cells, explain gestures, no silent dead-end**

In the cell-focus/dblclick path (`workbench.js:95-104`): set `data-cell-editable` on focusable cells only when `FileEditable` (JS must consult `data-file-status` wrapper already present); JSON/array/object cells and page-truncated files: on dblclick/Enter, call the record dialog open for that row instead of doing nothing. Add `title` to editable cells: `Enter to edit`; on first workspace visit set `sessionStorage['fluxport-hint-cell-edit']` and show a one-line `.edit-hint` bar above the table: `Tip: double-click a cell to edit. Escape cancels.` with a dismiss `×`.

- [ ] **Step 4: Test**

Run: `go build ./... && go test ./internal/web/...`
Add one render assertion if the editor-panel change lacks coverage: collection with `.Fields` renders `data-add-fields` inputs (follow `file_editing_test.go` style).

- [ ] **Step 5: Commit**

```bash
git add ui internal/web
git commit -m "ux: visible edit affordances and plain record form"
```

### Task 6: State language — drafts, save status, job states

**Files:**
- Modify: `ui/static/js/file-editor.js`, `ui/static/js/dataset-workspace.js`, `ui/static/js/file-workspace.js` (reload/source-changed strings), `ui/templates/partials/jobs.html`, `ui/templates/partials/metadata.html`
- Test: `internal/web/dataset_workspace_test.go`

**Interfaces:**
- Consumes: `data-file-edit-status` behavior contract (string changes only), draft storage keys `fluxport-cleanup-<id>` unchanged.
- Produces: draft restore opt-in only (no silent auto-apply of stored drafts).

- [ ] **Step 1: Save status copy**

`file-editor.js` strings: `Saving…` → `Saving change…`; `Saved · updating table…` → `Saved — refreshing table…`; failure `File saved; table needs refresh` → `Saved. The table will refresh shortly.`; `Change failed. Open Clean to retry.` → `Change failed — reopen Clean and apply again.` `workbench.js:65` reload notice text: `The source changed. Reload before editing.` → `The file changed outside FluxPort. Reload to continue editing.`

- [ ] **Step 2: Draft restore is a choice**

`dataset-workspace.js` (lines ~23-37): remove silent auto-restore on load; on detecting `localStorage['fluxport-cleanup-<id>']` render into `[data-draft-notice]`: `You have saved steps from an earlier visit.` + buttons `Restore` / `Discard` (Discard clears the key). Keep the existing manual draft save hook untouched.

- [ ] **Step 3: Job state words**

`jobs.html` `state-label` text mapping (template ternary on `.State`): `staged`→`Uploaded`, `queued/running`→`Working`, `finished/ready`→`Done`, `failed`/`cancelled` keep, `Downloaded` keep. Keep `data-state` attr raw. `workspace-advanced.html:21` `Next run (Unix seconds): {{.NextRun}}` → `Next run (unix time): {{.NextRun}}`.

- [ ] **Step 4: Test**

Run: `go test ./internal/web/...`
Expected: PASS (copy assertions updated only).

- [ ] **Step 5: Commit**

```bash
git add ui
git commit -m "ux: plain state language, opt-in draft restore"
```

### Task 7: Calm skin — tokens, tables, panels

**Files:**
- Modify: `ui/static/css/tokens.css`, `ui/static/css/components.css` (bound, append-first), `ui/static/css/workspace.css` (survivor conflicts)
- Test: no Go test asserts CSS; verify by `go build ./...` + manual dark/light check instructions in step.

**Interfaces:**
- Consumes: every existing class name.
- Produces: adjusted token values only; no selector renames.

- [ ] **Step 1: Token tune**

In `tokens.css` (both `:root` and `[data-theme=light]`): `--radius:10px`; add `--radius-sm:6px`, `--surface-veil` for hover tint; type scale: set `html{font-size:16px}` if currently different, and confirm `h1` in workspace header ≤1.35rem via components.css override (measure current — keep close, calm > big). One accent: point `--blue`, `--primary-bg` to existing `--accent`/`--accent-bg` values in light mode duplicates. Do NOT introduce new font-family.

- [ ] **Step 2: Table chrome**

In `components.css` table section: stripes `--stripe` opacity reduced (turn 50% transparent using color-mix if supported, else pick calmer hex); row hover → `--hover`; column header font-weight `600` instead of `bolder`; reduce header badge noise: type badges opacity 0.85. Panel flattening: `.panel` gets `border` instead of double shadow+border where both exist today (inspect current values first; remove shadow unless `.toolbar-popover, .workspace-more > div, dialog` floating layers).

- [ ] **Step 3: Verify both themes**

Run: `go build ./...`, start server (`go run ./cmd/fluxport serve` with a scratch data dir), visit `/collections`, open a file, view Summary; toggle theme; eyeball it for unreadable contrast on buttons/meta chips. Fix any `--faint`-on-`--panel` conflicts found.

- [ ] **Step 4: Commit**

```bash
git add ui/static/css
git commit -m "ux: calmer tokens, tables and panels"
```

### Task 8: Dead template tree removal

**Files:**
- Delete: `ui/templates/pages/detail.html`, `ui/templates/pages/insights.html`, `ui/templates/pages/quality.html`, `ui/templates/pages/changes.html`, `ui/templates/pages/download-review.html`, `ui/templates/partials/dataset-tabs.html`, `ui/templates/partials/dataset-header.html`
- Modify: `ui/static/js/app.js` (remove `app.js:28` `.tabs` toggle + its CSS in `components.css`), `ui/static/js/workspace.js` (remove legacy preview-table column checkbox code targeting dead markup, ~lines 24-41; keep converter + SQL insert helpers)
- Test: full suite

**Interfaces:**
- Consumes: subagent audit conclusion these templates have no render references (`internal/web/workspace_views.go:78` renders only `pages/workspace`).
- Produces: no `.tabs` CSS rules or JS hooks left.

- [ ] **Step 1: Prove dead before deleting**

Run: `grep -rn "dataset-tabs\|dataset-header\|pages/detail\|pages/insights\|pages/quality\|pages/changes\|pages/download-review\|\"tabs\"\|\.tabs" internal ui --include=*.go --include=*.html --include=*.js`
Expected: only the files being deleted plus the two JS/CSS hooks being removed. If any live render reference appears, STOP and report instead of deleting.

- [ ] **Step 2: Delete + strip hooks**

Delete the seven listed files; remove the `.tabs a` JS hook and both `.tabs` rule groups in `components.css`; remove dead column-checkbox block in `workspace.js`.

- [ ] **Step 3: Full test**

Run: `go test ./...`
Expected: ALL PASS.

- [ ] **Step 4: Commit**

```bash
git add -A ui
git commit -m "ux: remove dead parallel workspace templates"
```

### Task 9: Handover notes (deferred docs)

**Files:**
- Create: `docs/superpowers/plans/2026-10-17-ui-ux-followups.md`

- [ ] **Step 1: Write follow-ups file** listing: doc sync on owner's dirty files post-merge (README, dataset-workspace, reference — new tab names, download labels, draft opt-in), command palette + `?` shortcuts sheet, right-rail File panel, CSS file split, per-column single menu consolidation. One paragraph each.

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/plans/2026-10-17-ui-ux-followups.md
git commit -m "ux: record follow-up work"
```

## Self-Review notes

- Spec → task mapping: vocabulary (T1), spine toolbar (T2), clean outcome (T3), download (T4), edit affordances (T5), state language (T6), calm skin (T7), dead code (T8). Non-goals respected (no right rail, no palette).
- Type consistency: template funcs `fieldType` only added in T5 funcmap; JS hooks list in Global Constraints is the single source attr names checked against per task.
- Review Focus coverage: (1) T2 Step 2 grep; (2) T4 conditional option test via `download_flow_test.go`; (3) T3 Step 4 + test grep; (4) T7 Step 3; (5) T5 renders with read-only collection via existing tests.
- Proportion: plan ~3× spec length; bodies only where behavior must change (T4 Step 3 rule, T5 mini-form construction), rest are exact-value edits.

## Post-review record (2026-10-17)

Review verdict was "with fixes"; the findings were fixed in commit c05e93e (completed vocabulary sweep with extended assertion table, dead column controls removed from the live preview partial plus its stale usability-check steps, review polish). One deliberate deviation stands and is sanctioned: Task 4 Step 3 said the download form must not gain options at runtime, but `ui/static/js/file-workspace.js` still inserts a single "Changes preview" option when the browser holds an unsaved Clean draft — the draft lives in localStorage, which the server cannot see, so no server-rendered form can cover that case. The inserted option copies the server-rendered option text byte-for-byte; the exception needs the owner's decision only if the local-draft download path itself is ever changed.
