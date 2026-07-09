# Proposal: TUI All Schemas + Connection Clone Defaults

## Intent

Operators report the TUI does not show all database schemas and cannot see which schemas a saved connection uses for clone/dump defaults. Root causes: fixed-height schema panel with no scroll (items below the fold look “missing”), schema picker gated on `save_connections`, and connection list rows omitting `Connection.Schemas` even though profiles already drive `dolly clone --connection` / `dump --connection`.

## Scope

### In Scope
- Viewport scroll for schema picker and loaded table list (`SchemaDraft` offsets; `j`/`k`; “N more” indicators)
- Show `schemas` summary on saved connection list rows (truncate on narrow terminals)
- Offer schema picker after connect when resolved schemas are empty, independent of `save_connections` (auto-save still gated)
- Spec deltas for `dolly-tui`; align help/statusbar with real scroll behavior
- Coordinate with `schema-selection-for-clone` (CLI/TUI clone) — no duplicate CLI flags here

### Out of Scope
- CLI `dolly clone --schemas`, auto-discovery default, interactive clone prompt → `schema-selection-for-clone`
- TUI clone execution flow → `schema-selection-for-clone` Phase 2
- Connection-screen live DB multi-select picker (F6) — optional Phase 2 / follow-up
- Changing `dolly-connections` empty-schemas = public-only semantics without user decision

## Capabilities

### New Capabilities
- None

### Modified Capabilities
- `dolly-tui`: Scrollable schema picker and table list; connection list shows profile `schemas`; picker when profile/manual schemas unset regardless of `save_connections`; document that profile schemas are clone/dump defaults when using `--connection`
- `dolly-connections`: Delta only if spec must state list-display contract for `schemas` on profiles (otherwise TUI-only)

## Approach

**Phase 1 (this change):** Mirror `dumpScreen.logTailOffset` pattern in `SchemaDraft` (`pickerScrollOffset`, `tableScrollOffset`). Fix `keys.go`/`statusbar.go` hints. Extend `connections/display.go` for compact schema summary on list rows. In `connection.go`, set `schemaPickerMode` when post-connect schemas are empty, not only when `save_connections` is true.

**Phase 2 (optional):** Live schema picker on connection screen to edit profile defaults without visiting schema screen.

**Coordination (`schema-selection-for-clone`):** This change owns TUI visibility and profile schema display; sibling change owns CLI selection and TUI clone wiring. Both may touch `internal/tui/schema.go` — land scroll/list first or rebase; profile `schemas` remain the shared clone default for `--connection`.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `internal/tui/schema.go` | Modified | Viewport scroll rendering |
| `internal/tui/screen.go` | Modified | `SchemaDraft` scroll fields |
| `internal/tui/keys.go`, `statusbar.go` | Modified | Scroll bindings and hints |
| `internal/tui/connection.go` | Modified | Picker gate; list row schema text |
| `internal/connections/display.go` | Modified | `DisplaySchemasSummary` |
| `internal/tui/*_test.go`, `testdata/*.golden` | Modified | Scroll and list goldens |
| `openspec/changes/.../specs/dolly-tui/` | New delta | Requirements for scroll + list |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| PR overlap with `schema-selection-for-clone` / `tui-connections-crud` | Med | Sequence PRs; narrow file overlap |
| 400-line budget exceeded with Phase 2 picker | Med | Ship Phase 1 only in v1 |
| Picker without `save_connections` surprises public-only users | Med | Document; empty selection still defaults per `schemaFilter` |
| Missing schemas are DB privileges, not UI | Low | No change to `ListPostgresSchemaNames` |

## Rollback Plan

Revert TUI commits. Viewport clipping and `save_connections`-gated picker return; connection list hides schemas again. No migrations; YAML profiles unchanged.

## Dependencies

- Exploration: `sdd/tui-all-schemas-connection-clone-defaults/explore`
- Parallel: `schema-selection-for-clone` (CLI/TUI clone — do not duplicate)

## Success Criteria

- [ ] Picker with 30+ schemas: all reachable via scroll at 80×24 and 60×20 goldens
- [ ] Loaded table list scrolls when content exceeds panel height
- [ ] Saved connection rows show schema summary when `schemas` non-empty
- [ ] Manual connect with empty schemas opens picker when `save_connections` is false
- [ ] `go test ./internal/tui/...` passes; help text matches behavior

## Phases

| Phase | Deliverable |
|-------|-------------|
| 1 | Scroll + list schema display + decouple picker gate |
| 2 (optional) | Connection-screen schema picker for profile edit |
