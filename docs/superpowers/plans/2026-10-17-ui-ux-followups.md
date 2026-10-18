# UI/UX follow-ups after the 2026-10-17 simplification branch

These items were explicitly deferred from docs/superpowers/plans/2026-10-17-ui-ux-simplification.md to keep that branch reviewable. Each paragraph below is standalone: if you pick one up, nothing else here is a prerequisite.

Status as of 2026-10-18: all five items shipped (see the "What shipped" record at the end). Two vocabulary rulings in item 1 remain the owner's call.

## 1. Sync the docs after the branch merges (shipped in 0ac00db)

`README.md`, `docs/dataset-workspace.md`, and `docs/reference.md` were updated to the shipped vocabulary and flows after the owner committed their own edits: four workspace tabs (Data · Clean · SQL · Summary), the single ⋯ menu (File history / File settings / API settings / Saved views / Processing history), Clean copy, download labels, draft restore, job state words, and the plain add-record form. Historical changelog sections (0.3.14–0.3.20 in `docs/reference.md`) were deliberately left as written history, and the data-engineering guide keeps its own "recipes" vocabulary because its CLI/API domain language is unchanged.

Still the owner's call: whether the upload-status words ("preview"/"rejected") and the file-status words in File settings should join the Uploaded/Working/Done map, and whether "Saved steps" buttons that POST to `/pipelines/…` routes should advertise a friendlier phrase.

## 2. Command palette and a `?` shortcuts sheet (shipped in eecbadd)

Both exist as native `<dialog>`s in `ui/templates/layouts/base.html`, wired from `ui/static/js/app.js`: `Ctrl`/`⌘`+`K` opens the palette and `?` opens the shortcut sheet (Escape closes both; focus returns to the opener because native `dialog` restores it). Commands are read from the page's own links at open time — every `[data-workspace-target]` view link, `[data-editor-open]` editor action, `[data-recent-files]` recent file, the `#import` picker, plus a static list of global pages — so the palette cannot drift from the rendered UI and htmx fragment swaps need no re-scan. Actions use the words already on screen; no new vocabulary. The `?` sheet lists only shortcuts that already exist (double-click edits, Enter confirms, clicking away saves, Escape cancels, arrows and Shift-select, copy, Undo/Redo).

Note: `<input type="search">` swallows Escape for its clear-field behavior, so the palette closes Escape explicitly.

## 3. Right-rail File panel (shipped)

A right rail (`partials/file-rail.html`) gathers the file context that used to sit above and below the sheet: file facts (rows, size, status, format, filename, updated), the applied Saved steps chain, the three most recent File history entries with a link to the full view, a quick Convert / Download link, and the Processing history disclosure, which moved out of the page footer. `#collection-metadata` left the header meta row, so the toolbar is now navigation and actions only. The layout is a grid in `workspace.css`: a 296px sticky rail beside the sheet at 1180px and wider, stacking above it on narrow screens with no JavaScript and no hidden content. All rendering stays server-side; the existing `data-*` hooks (`#collection-metadata`, `#activity`) were relocated, not renamed, so the out-of-band metadata swap after saves and the `#activity` deep links keep working.

## 4. Split `components.css` (shipped in 1c03ba2, with a deliberate deviation)

`ui/static/css/components.css` was split into four contiguous slices — `components.css` (shell, sidebar, topbar, buttons, fields), `content.css` (sections, panels, pipelines, download chooser), `tables.css` (dataset views, cleanup, tables, settings, explorer), `pages.css` (responsive tail and later additions) — plus the existing `tokens.css`, `workspace.css` and `editor.css`. `workspace.css` keeps the workspace spine, including the file settings and file history panels, so its "decide what it keeps" question is settled.

Deviation from this plan, deliberate and verified: the first attempt bucketed rules by selector family (layout/controls/tables/pages) as originally sketched, but families interleave through the file, and moving them across files changes the cascade — a rendered comparison across 12 pages × 2 themes showed 12 of 24 screenshots changed. The shipped split is therefore contiguous and byte-identical: concatenating the four files reproduces the original exactly, so load order in `layouts/base.html` cannot change any rule's precedence. While in the files, 44 top-level rules whose classes appear in no template, script or rendered DOM were deleted (`.stat-card`, `.collection-more`, `.export-bar`, …).

## 5. One menu per column (shipped in 557e41c)

Every column header now carries one `<details class="column-menu">` with Sort ascending / Sort descending / Filter this column (explicit URLs built from the same query state as the sort link), Hide column (reuses the existing `prefs` bookkeeping and the Columns panel as the source of truth for re-showing), Copy values, and — on editable files only — Rename column (the existing editor command). The panel is `position: fixed` and placed by JS on open because the `th`'s `overflow: hidden` and the table's scroll container clip absolutely-positioned children; without JS the panel still opens at its static position rather than being clipped. Hidden-column bookkeeping stays in `prefs`, and the add-record form follows the visible columns as planned.

## What shipped (record)

- `1becfe7` ux: save cell edits when focus leaves the editing input
- `6081638` dev: make the published Docker port reachable and add a theme contact sheet
- `fba868b` ux: follow the system theme and mark the current workspace tab
- `557e41c` ux: give every column one menu for sort, filter, hide and copy
- `eecbadd` ux: add a command palette and keyboard shortcut sheet
- `1c03ba2` ux: split the component stylesheet and drop rules nothing renders
- `7390daa` ux: list recent files and import in the palette; record follow-up status