## Exploration: TUI All Schemas + Connection Clone Defaults

### Current State

**Schema discovery (backend)** — `internal/db/postgres.go` already distinguishes two operations:
- `ListPostgresSchemaNames` — queries `information_schema.schemata`, excludes `pg_catalog`, `information_schema`, `pg_toast`, and `pg_temp_*` / `pg_toast_*` patterns. Returns **all** user-visible schema names (no count cap in SQL).
- `LoadPostgresSchemas` — loads tables for given schema names; when `schemas` is nil/empty, `schemaFilter` defaults to **`["public"]` only**.

**TUI connect → schema picker** (`internal/tui/connect.go`, `app.go`, `connection.go`):
- `ConnectForSchemaPicker` calls `ListPostgresSchemaNames` and passes the full slice to `SchemaDraft.AvailableSchemas`.
- Picker activates when `schemaPickerMode` is true (after connect).
- **Gate:** manual connect from fields sets `schemaPickerMode := saveConnections` (`connection.go:321`). If `save_connections: false`, connect skips the picker and loads **public only** — operator never sees other schemas.
- Picking a saved profile with **non-empty** `schemas` skips the picker and loads only those schemas (`connectSchemasForProfile`).
- Picking a profile with **empty** `schemas` opens the picker (when `save_connections: true`).

**TUI schema screen rendering** (`internal/tui/schema.go`):
- Picker and post-load table list render **every** schema/table as lines inside a lipgloss panel with fixed `Height(contentH)`.
- **No scroll offset** (`PickerCursor` only highlights; content above/below viewport is clipped).
- `schemaScreen.Update` ignores `j`/`k` when `PickerActive` is false — loaded table names are not scrollable either (help text in `keys.go` says "scroll table list" but behavior is a no-op).
- **Likely user-visible symptom:** databases with many schemas/tables appear to "not show all" because items below the terminal fold are unreachable.

**Saved connections + default schemas for clone/dump** (`internal/connections/`, `connection.go`):
- `Connection.Schemas []string` is persisted in YAML and used by CLI `dolly clone --connection` / `dump --connection` (`runCloneWithSource` passes `conn.Schemas`; empty → public-only downstream).
- TUI list rows: `name + DisplaySummary(host/user/db)` only — **schemas are not shown** in the list (`display.go`).
- Schemas are editable offline via **F2 edit panel** as a comma-separated `Schemas:` text field (`connPanelEdit`, `parseCommaSchemas`) — no multi-select picker on the connection screen.
- After schema-screen picker confirm, selections persist to profile via `UpsertBySignature` (`app.go:244-252`).

**Related SDD (overlapping, not duplicate)**:
- `openspec/changes/schema-selection-for-clone/` — CLI `--schemas`, auto-discovery for clone, TUI clone flow (shell-only today).
- `openspec/changes/tui-connections-crud/` — discoverability + F2 field edit (partially landed in code).

**Spec drift**: Main `openspec/specs/dolly-tui/spec.md` still describes public-only connect; delta in `tui-connections-crud/specs/dolly-tui/spec.md` documents picker + saved profiles but not viewport limits or connection-list schema display.

### Affected Areas

| Path | Why |
|------|-----|
| `internal/tui/schema.go` | Add viewport scroll for picker + table list |
| `internal/tui/screen.go` | `SchemaDraft` scroll offsets |
| `internal/tui/keys.go`, `statusbar.go` | Picker scroll hints; fix misleading "scroll table list" |
| `internal/tui/connection.go` | Decouple picker from `save_connections`; list row schema summary; optional picker in edit |
| `internal/tui/connection.go` `View` | Show `schemas` on saved-profile rows |
| `internal/connections/display.go` | Optional `DisplaySchemasSummary(c)` |
| `internal/tui/*_test.go`, `testdata/*.golden` | Scroll + list display goldens |
| `openspec/specs/dolly-tui/spec.md` | Merge picker, scroll, connection-list schema requirements |
| `openspec/changes/schema-selection-for-clone/` | Coordinate: profile `schemas` = clone default already; avoid duplicate CLI work |

### Approaches

#### 1. Viewport scroll for schema picker and table list (recommended first)

Add `pickerScrollOffset` / `tableScrollOffset` to `SchemaDraft` (mirror `dumpScreen.logTailOffset`). `j`/`k` scroll the visible window; cursor stays in view. Show `↑N more` / scroll indicator when clipped.

- **Pros**: Fixes primary "not all schemas" UX bug without DB changes; small, testable; works for any schema count from `ListPostgresSchemaNames`.
- **Cons**: Does not improve connection-list schema editing.
- **Effort**: Medium (~250–400 LOC + goldens)

#### 2. Always offer schema picker when profile/schemas unset (decouple from `save_connections`)

Change `connectFromDraft` so `schemaPickerMode` is true whenever resolved schemas are empty, not only when `save_connections` is true. Keep auto-save gated on flag.

- **Pros**: Operators without saved connections still see all schemas; aligns with user expectation.
- **Cons**: Extra connect + list query for users who only want `public`; behavior change from today.
- **Effort**: Low (~20–40 LOC + tests)

#### 3. Show schemas on connection list + keep F2 comma edit

Extend list rows: `staging  host/*** / user/*** / db/***  schemas: app, billing` (truncate with `+N` on small terminals). F2 edit unchanged.

- **Pros**: Satisfies "schemas in connections list" for clone defaults visibility; minimal risk.
- **Cons**: Comma edit still error-prone for many schemas; no live DB name validation on edit panel.
- **Effort**: Low (~80–150 LOC)

#### 4. Connection-list schema picker (connect → list names → multi-select → save profile)

On saved list: new action (e.g. `F6` or sub-mode) runs ping + `ListPostgresSchemaNames`, reuses `SchemaDraft` picker UI on connection screen or modal, writes `Connection.Schemas` without full schema load.

- **Pros**: Best UX for "select which clone by default"; reuses existing picker patterns.
- **Cons**: Requires live DB; overlaps schema-screen picker; more UI state on connection screen.
- **Effort**: Medium–High (~400–600 LOC)

#### 5. Full merge with `schema-selection-for-clone`

Single change: scroll + connection-list schemas + CLI clone flags + TUI clone flow.

- **Pros**: One coherent "schemas everywhere" delivery.
- **Cons**: Exceeds 400-line PR budget; mixes TUI fix with large clone feature.
- **Effort**: High (1000+ LOC)

### Recommendation

**Ship as a focused TUI change** (this change name), coordinating with `schema-selection-for-clone` for CLI/TUI clone:

1. **Must-have:** Approach **1** (viewport scroll) — root cause for missing schemas in the TUI.
2. **Should-have:** Approach **2** (picker not gated on `save_connections`) — unless product wants public-only without saved profiles.
3. **Should-have:** Approach **3** (schemas visible on connection list rows) — satisfies "en la lista de connections" without new screens.
4. **Could-have (separate task or Phase 2):** Approach **4** — picker on connection screen for editing defaults without visiting schema screen.
5. **Defer:** CLI clone `--schemas` / TUI clone execution to `schema-selection-for-clone` — profile `schemas` already feeds `dolly clone --connection -ff`.

**Clarify with user:** "schemas que se clonan por defecto" = existing `Connection.Schemas` on saved profiles (used by CLI clone today), or a new field separate from dump filter?

### Risks

- **Terminal size:** Even with scroll, 60×20 remains tight; need compact indicators and tests at both golden sizes.
- **Behavior change:** Decoupling picker from `save_connections` changes default from public-only to explicit picker for manual connect.
- **Overlap with active changes:** `schema-selection-for-clone` and `tui-connections-crud` may touch same files — sequence PRs or merge deltas.
- **400-line budget:** Scroll + list display + decouple may fit one PR; connection-list picker likely needs a chained PR.
- **Permissions:** `ListPostgresSchemaNames` only returns schemas visible to the connected role; missing schemas may be privilege-related, not UI.

### Ready for Proposal

**Yes** — propose scoped delivery:
- Fix schema picker/table viewport scrolling.
- Show `schemas` on saved connection list rows; document that they drive `clone`/`dump --connection` defaults.
- Optionally enable picker without `save_connections`.
- Point CLI clone improvements to `schema-selection-for-clone` to avoid duplication.

Ask user to confirm: (1) empty `schemas` on profile = "clone all discovered schemas" vs "clone public only"; (2) whether connection-list multi-select picker is required in v1 or list display + F2 text is enough.
