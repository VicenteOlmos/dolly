## Exploration: Clone Target DSN Selection, Auto-Name, and Analyze Strategy

### Current State

The clone TUI form (`internal/tui/clone.go`) has 3 plain text fields: **Clone name**, **Target DSN**, and **Strategy**. All are free-text with basic cursor navigation. No picker, no auto-defaults, no dropdown:

```
Target & strategy:  ↑/↓/Tab field · ←/→ edit · Esc back
  Clone name: (empty)
  Target DSN: (empty)
  Strategy: (empty)
```

**Clone name** — never auto-populated. The backend (`clonework/run.go:44-47`) generates it from `clone.CloneName(sourceDB, template)` only when the submitted name is empty, but the TUI never pre-fills it.

**Target DSN** — plain input. The backend (`clone/inprocess.go:123-134`) rewrites it with the clone name as the database path when empty. There is no mechanism to pick saved connections or auto-fill the current session's DSN.

**Strategy** — plain input. The backend supports exactly 4 strategies (`clone/strategy.go`): `template`, `schema-replay`, `logical-stream`/`copy-stream`/`streaming-copy`, `physical-backup`/`replication`. No analyze step exists.

**Saved connections** — `internal/connections/store.go` has a complete `ConnectionStore` interface with `FileStore` backend. The connection screen (`internal/tui/connection.go`) already has a saved-profiles list panel with CRUD. The TUI already seeds the schema picker from the active profile after connect.

**Clone name generation** — `internal/clone/clone.go:32-37`: `CloneName(sourceDB, template)` does `strings.ReplaceAll(template, "{db}", sourceDB)`. Default template in config: `{db}_kloned_1`. The user wants `{db}_dolly_{n}` with auto-incrementing number.

**Config** — `config.jsonc` has `clone.name_template`, `clone.target_url`, `clone.strategy` fields. The config screen already exposes `clone.name_template` as an editable string and `clone.strategy` as a choice field with the 4 strategies.

### Affected Areas

- `internal/tui/clone.go` — **Primary**: clone form model, field layout, rendering, overview rows, form section, field handling. Must add DSN picker panel, auto-name logic, strategy choice + analyze flag.
- `internal/tui/clone_run.go` — **Secondary**: may need to pass analyze info or change the clone runner interface if analyze runs pre-clone.
- `internal/tui/app.go` — **Secondary**: `handleCloneRequested()` must pre-fill SourceDSN into the new Target DSN current-DSN option. May need to pass `ConnectionStore` to clone screen. Must wire the DSN picker interactions.
- `internal/tui/screen.go` — **Minor**: `CloneDraft` struct may need new fields (e.g., `TargetDSNSource`, `AnalyzeEnabled`).
- `internal/tui/screen_nav.go` — **Minor**: may need additional section for picker/analyze panels.
- `internal/tui/connect.go` — **Minor**: `ConnectionDraft.DSN()` already exists and is used by app.go for SourceDSN.
- `internal/clone/clone.go` — **No change**: `CloneName()` already does the template substitution. Only the TUI needs to call it on init.
- `internal/clonework/run.go` — **No change**: already supports empty name (auto-generates) and empty target (falls back to config).
- `internal/config/config.go` — **Minor change**: default `NameTemplate` could be updated from `{db}_kloned_1` to `{db}_dolly_{n}` or the user can set it.
- `internal/tui/golden_test.go` + golden files — **Test updates**: all clone golden fixtures will need regeneration.
- `internal/tui/app_clone_test.go` — **Test updates**: new test cases for DSN picker, auto-name, analyze flow.
- `openspec/explore/clone-target-dsn-selection-default-name-analyze/` — This artifact.

### Approaches

1. **Augment clone form with sub-panels (recommended)** — Keep the 3-field form but make Target DSN and Strategy interactive:
   - **Target DSN field**: Press Enter to open a picker (like connection screen's list panel) with 3 choices: (1) Manual input (shows text editor), (2) Saved lists (shows ConnectionStore profiles), (3) Current DSN (auto-fills from session). Default to option 3.
   - **Clone name**: Auto-populate when entering the clone screen using `clone.CloneName(a.conn.Database(), cfg.Clone.NameTemplate)`. Keep it editable.
   - **Strategy**: Replace plain text with a choice selector (like config screen does — left/right arrows cycle). Add `(empty)` → default to config value. Add a modifier key (e.g. `a`) to enable/disable analyze step, shown as `Strategy: schema-replay [analyze]`.
   - Uses existing patterns: connection screen's panel system, config screen's choice cycling.
   - Pros: Consistent with existing UI patterns (connection screen has panels, config screen has choices). Reuses `ConnectionStore` directly. Minimal new code. Auto-name reuses existing `CloneName()` function.
   - Cons: More complex field interaction model. Requires adding section/panel state to clone screen.
   - Effort: **Medium** (~300-400 lines total across clone.go, app.go, screen.go)

2. **Separate DSN picker modal** — When Tab to Target DSN, pressing Enter opens a modal/popup with the 3 options. Same approach but using the modal system instead of embedded panels.
   - Pros: Simpler state management (modal is self-contained). Clearer UX boundary.
   - Cons: Modal system is currently only used for confirm/cancel dialogs. Not designed for data-entry pickers. Would need new modal type or significant modal refactor. Breaks the current section-navigation pattern.
   - Effort: **Medium-High** (~400-500 lines, plus modal refactor)

3. **Three separate fields with dedicated views** — Replace the Target DSN field with 3 sub-fields internally (source selector + DSN value). The strategy field becomes a choice + checkbox.
   - Pros: Very explicit model. Easy to test.
   - Cons: Inflates the form from 3 fields to 5+. More complex field navigation. The overview summary becomes confusing.
   - Effort: **Medium** (~300 lines) but more complex UX.

### Recommendation

**Approach 1: Augment with sub-panels.** It's the most consistent with existing patterns — the connection screen already has the list panel pattern, and the config screen already has the choice cycling. The clone screen already has section navigation (`cloneSectionForm`, `cloneSectionPicker`, `cloneSectionLog`), so adding a DSN picker as another interactive section fits naturally.

Specific implementation sketch:
1. **Auto-default Clone Name** — In `newCloneScreen()` or in `handleConnectResult()` when the app transitions to the clone screen, call `clone.CloneName(sourceDB, cfg.Clone.NameTemplate)` and pre-fill `draft.CloneName`. The app already has `cfg` and `a.conn.Database` available.
2. **Target DSN picker** — Add a new panel state to `cloneScreen` (like `connPanelList`, `connPanelFields`). When the user presses Enter on the Target DSN field, switch to DSN picker mode showing 3 options. Option 1 ("Manual"): back to text editor. Option 2 ("Saved"): show ConnectionStore profile list. Option 3 ("Current DSN"): auto-fill from session and return to form.
3. **Strategy choice** — Replace the text field with a choice cycler. Use the config screen pattern: left/right arrows cycle through the 4 strategies. Default to whatever is in `cfg.Clone.Strategy`.
4. **Analyze step** — Add an `[analyze]` toggle to the Strategy line. Pressing a key (e.g. `a`) toggles analyze mode. When enabled, before running clone, query the source for table count and database size and display as a preflight step. This can live as a new field in `CloneDraft` (`Analyze bool`) and new logic in `clonework.Run()` or in `app.handleCloneRequested()`.

### Risks

- **Field cursor interaction complexity** — Adding a picker panel means the form now has mixed interaction modes (text editing vs. list selection vs. choice cycling). Must carefully manage focus state to avoid confusing the user.
- **Saved DSN overhead** — The clone screen currently doesn't have access to `ConnectionStore`. The picker needs it passed in (like connection screen gets it). The app already has `connStore` so passing it is trivial, but it couples clone screen to the store.
- **Analyze step cost** — Querying table count and size requires a live DB query. This should be async (spinner during analyze) and cached or it'll feel slow. The preflight system already handles some async checks, so this can piggyback.
- **Golden file churn** — Every clone golden fixture changes because the form now auto-fills values. All clone-related `.golden` files need regeneration and review.

### Ready for Proposal

Yes. The investigation is complete. All affected files, patterns, and approaches are identified. The orchestrator can proceed with proposal with high confidence.

Key things to confirm with user before proposal:
1. Exact default name template format: `{db}_dolly_{n}` or keep configurable via `name_template`?
2. Analyze: show stats inline during preflight or as a separate confirmation step before clone starts?
3. Saved connections for Target DSN: include all saved profiles or only ones matching the source host?
