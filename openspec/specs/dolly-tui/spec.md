# dolly TUI Specification

## Purpose

Provide an interactive terminal cockpit for dolly database operations. The application delivers connect → schema → dump/clone workflow: live PostgreSQL connection and schema introspection, plus in-process dump and clone execution via `internal/dump` and the clone layer.

## Requirements

### Requirement: Interactive application shell

The system MUST launch `dolly tui` into a managed full-screen TUI and MUST show Connection as the initial view.
(Previously: Welcome opened first.)

#### Scenario: Application starts in a TTY

- GIVEN stdout is a terminal
- WHEN the operator runs `dolly tui`
- THEN a full-screen session starts on Connection

#### Scenario: Resize

- GIVEN the session is active
- WHEN dimensions change
- THEN visible content remains renderable

### Requirement: Workflow screens and field presentation

The application MUST provide Connection, Schema, Dump, Clone, and Config workflows without a Welcome gate. Non-empty passwords MUST render as exactly eight `*`. Schema MUST show loaded database state plus a next-step hint.
(Previously: only Connection, Schema, Dump, and Clone; Config screen absent.)

#### Scenario: Connection fields

- GIVEN Connection is active
- WHEN the operator views it
- THEN fields and enabled saved-profile list are visible
- AND non-empty Password renders as `********`

#### Scenario: Schema next step

- GIVEN Schema is active after connect
- WHEN the operator views it
- THEN loaded tables and a dump/clone next-step hint appear

#### Scenario: Dump screen supports live dump

- GIVEN Dump is active
- WHEN controls render
- THEN path, transaction, log, and run controls are visible

#### Scenario: Config screen appears in workflow

- GIVEN Config (screen 5) is navigated to via `5`
- WHEN the operator views it
- THEN all 24 configuration knobs are visible, scrollable, and grouped by section
### Requirement: Live PostgreSQL connection

The connection screen MUST compose a URL-encoded DSN from Host, Port, Database, User, and Password. Enter MUST async connect: open, ping, list source schema names for dump and clone pickers, and load introspection tables using profile `schemas` or `public` when profile `schemas` is empty. On success retain `*sql.DB`, populate the browse-only schema screen, auto-save connection fields when `save_connections` is true, and navigate to schema. On failure stay on connection with errors. Ping-only paths MUST NOT auto-save. Connect MUST NOT navigate to a schema multi-select picker.
(Previously: connect could enter `schemaPickerMode` and require schema multi-select on the schema screen before other workflows.)

#### Scenario: Successful connect and schema browse

- GIVEN valid fields and reachable PostgreSQL
- WHEN the operator presses Enter
- THEN connecting hint appears
- AND after success the schema screen shows loaded table metadata
- AND no schema multi-select picker is shown on the schema screen

#### Scenario: Connection failure

- GIVEN invalid details
- WHEN the operator presses Enter
- THEN the connection screen stays active with errors

#### Scenario: Password URL encoding

- GIVEN a password with special URL characters
- WHEN the operator connects
- THEN the DSN encodes credentials correctly
### Requirement: Test connection without schema load

Ping-only test MUST NOT load schema, retain session `*sql.DB`, or change screens. On the fields or edit panel `Ctrl+T` MUST run ping from field DSN. When the saved list panel is active, `t` MUST run ping from the current draft. When `save_connections` is false, `Ctrl+T` on the connection screen MUST still run ping from fields. Duplicate submits while busy MUST be ignored. Enter remains full connect.
(Previously: `t` on connection screen always; no panel distinction.)

#### Scenario: Successful test from fields

- GIVEN the fields panel is active
- WHEN the operator presses `Ctrl+T`
- THEN ping succeeds with status-bar feedback only
- AND no schema load or navigation occurs

#### Scenario: Successful test from list

- GIVEN the saved list panel is active and `save_connections` is true
- WHEN the operator presses `t`
- THEN ping runs from the draft without appending to fields

#### Scenario: Busy guard blocks overlapping operations

- GIVEN async connect or test in progress
- WHEN the operator presses `t`, `Ctrl+T`, or Enter again
- THEN no additional operation starts
### Requirement: Schema summary from loaded database

After connect, the schema screen MUST show counts and sorted table names for the active introspection filter—not mocks. The schema screen MUST be browse-only: it MUST NOT require multi-select confirmation to proceed to dump or clone.
(Previously: schema screen could host a mandatory multi-select picker after connect.)

#### Scenario: Schema summary after connect

- GIVEN successful connect with profile filter `[app]`
- WHEN the schema screen is active
- THEN counts and names reflect `app` only

#### Scenario: Schema without prior connection

- GIVEN no successful connection
- WHEN the operator opens the schema screen
- THEN the screen prompts to connect first
### Requirement: Testable schema loading seam

The TUI MUST expose an injectable schema-loader abstraction so tests simulate connect and ping success and failure without live PostgreSQL.

#### Scenario: Unit tests without live database

- GIVEN tests inject a fake schema loader
- WHEN connect and test-connection messages are exercised
- THEN connect success, connect failure, ping success, ping failure, and navigation are asserted
- AND default `go test ./internal/tui/...` requires no PostgreSQL


### Requirement: Injectable connection store seam

The TUI MUST accept an injectable `ConnectionStore` so tests exercise list, pick, save-as, auto-save, rename, delete, field-edit-save, and duplicate-name rejection without a real store file.

#### Scenario: Unit tests without store file

- GIVEN tests inject a fake connection store
- WHEN connection-screen messages for list, pick, save-as, auto-save, rename, delete, and edit-save run
- THEN outcomes are asserted without reading `.dolly.connections.yaml`

### Requirement: Saved connection list when enabled

When `save_connections` is true, the connection screen MUST show a saved-profile list (or empty-state hint). Rows MUST show profile `name` and masked host, user, and database; password MUST NOT appear. Picking a profile MUST populate `ConnectionDraft`; Enter MUST connect via the same async flow as manual entry.

#### Scenario: List with masked details

- GIVEN `save_connections` is true and profiles exist
- WHEN the connection screen is active
- THEN each row shows name plus masked host, user, and database
- AND no password is visible

#### Scenario: Pick populates draft

- GIVEN a saved profile in the list
- WHEN the operator selects it and presses Enter to connect
- THEN `ConnectionDraft` matches the profile
- AND connect proceeds as for manual entry

#### Scenario: Empty list hint

- GIVEN `save_connections` is true and the store is empty
- WHEN the connection screen is active
- THEN a hint directs manual connect (auto-save will create a profile)

#### Scenario: Flag false unchanged

- GIVEN `save_connections` is false
- WHEN the connection screen is active
- THEN only manual fields are shown
- AND no saved list or store I/O occurs

### Requirement: Saved-profile actions

When `save_connections` is true, saved-list actions MUST use letters: `e` edit, `s` save-as, `r` rename, `d` delete, `t` ping. `F2`/`F3`/`F4`/`F5`/`F9` MUST NOT run them. Printable keys MUST type in text panels. Delete MUST require confirmation.
(Previously: profile actions used function keys; letter actions were prohibited; delete was immediate.)

#### Scenario: Letters type in fields

- GIVEN the fields panel with Host focused
- WHEN the operator presses `j` then `t`
- THEN Host contains `jt`
- AND no profile action runs

#### Scenario: Letters run list actions

- GIVEN the saved list panel is active
- WHEN pressing `e`, `s`, `r`, `d`, or `t`
- THEN the matching list action is requested

#### Scenario: Function keys ignored for list actions

- GIVEN the saved list panel is active
- WHEN pressing `F2`, `F3`, `F4`, `F5`, or `F9`
- THEN no profile action starts

#### Scenario: Delete is confirmed

- GIVEN a saved profile is selected
- WHEN pressing `d`
- THEN deletion waits for modal acceptance

### Requirement: Edit saved profile fields without connect

When `save_connections` is true, the operator MUST edit the highlighted profile's host, port, database, user, password, and schemas from the saved list via `e` without connecting and without save-as. An edit panel MUST reuse the connect field strip. `Enter` MUST persist the existing profile name via `ConnectionStore.Put`. `Esc` MUST cancel with no store write. Save MUST NOT require connect or a new profile name. List rows MUST keep masked summaries after save.

#### Scenario: Edit opens populated panel

- GIVEN profile `staging` is selected in the list
- WHEN the operator presses `e`
- THEN an edit panel shows `staging`'s stored fields (password editable, not echoed in list)
- AND the operator remains on the connection screen without connect

#### Scenario: Save persists without connect

- GIVEN the edit panel is open for `staging` with changed host
- WHEN the operator presses Enter
- THEN `ConnectionStore.Put` updates `staging` in the store
- AND no connect, schema load, or save-as prompt runs
- AND the list reflects masked updated details

#### Scenario: Esc cancels edit

- GIVEN the edit panel is open with unsaved changes
- WHEN the operator presses `Esc`
- THEN the panel closes
- AND the store entry is unchanged

#### Scenario: Edit does not create a new profile

- GIVEN profile `staging` exists
- WHEN the operator edits fields and saves
- THEN the store still has one profile named `staging`
- AND no save-as name prompt appears

#### Scenario: Save fails without encryption key

- GIVEN encryption is required and `DOLLY_CONNECTIONS_KEY` is unset
- WHEN the operator saves from the edit panel
- THEN save fails with a clear error
- AND no partial write occurs

### Requirement: Auto-save after successful connect

When `save_connections` is true, after successful connect via Enter the system MUST upsert by `(host, port, database, user)` per `dolly-connections`. Save-as MUST still reject duplicate names.

#### Scenario: Auto-save on connect

- GIVEN `save_connections` is true and no profile matches the signature
- WHEN the operator connects successfully via Enter
- THEN a profile is created or updated without a name prompt

#### Scenario: Test connection does not auto-save

- GIVEN `save_connections` is true
- WHEN ping-only test succeeds (`Ctrl+T` on fields or edit, or `t` on list)
- THEN no store write occurs

### Requirement: Explicit save-as

When `save_connections` is true, save-as MUST prompt for a new profile name and write the current draft fields.

#### Scenario: Save-as persists profile

- GIVEN valid draft fields
- WHEN save-as with a unique name
- THEN the profile is written and appears in the list

#### Scenario: Duplicate save-as name rejected

- GIVEN profile `staging` exists
- WHEN save-as with name `staging`
- THEN save fails with duplicate-name error
- AND no overwrite dialog is shown

### Requirement: Rename saved profile

When `save_connections` is true, rename MUST call `ConnectionStore.Rename` and refresh the list.

#### Scenario: Rename updates list

- GIVEN profile `old` is selected
- WHEN renamed to unique `new`
- THEN the store has `new` not `old`
- AND the list shows `new`

### Requirement: Delete saved profile

When `save_connections` is true, delete MUST remove the selected profile from the store and list.

#### Scenario: Delete removes entry

- GIVEN a profile is selected
- WHEN the operator deletes it
- THEN it is removed from store and list

### Requirement: Schema selection for empty profile schemas

When profile `schemas` is empty, dump and clone pickers MUST start with no schemas pre-selected. Operators MUST select schemas on the dump or clone screen before starting those operations. Operators MAY also set profile `schemas` via the connection edit panel `Schemas:` field (F2); on next connect those names pre-select both pickers. Clone and dump schema selection MUST NOT occur on the schema screen.
(Previously: empty profile `schemas` with `save_connections` required multi-select on the schema screen before dump; selections persisted to the profile.)

#### Scenario: Dump requires dump-screen selection

- GIVEN connected session, empty profile `schemas`, and dump screen active
- WHEN the operator starts dump with no schemas selected in the dump picker
- THEN no dump execution occurs
- AND the status bar asks the operator to select schemas on the dump screen

#### Scenario: Fixed schemas on profile pre-select pickers

- GIVEN profile `schemas: [app]`
- WHEN connect completes
- THEN `app` is pre-selected in both dump and clone pickers
- AND only `app` tables appear on the schema browse screen

### Requirement: Saved-profile discoverability

When `save_connections` is enabled in config, documentation and in-app hints MUST tell operators how to enable the feature and use section overview keys (`↑`/`↓`, `Enter`, `Esc`), list keys (`↑`/`↓`, `e`/`s`/`r`/`d`/`t`, Enter), and `Ctrl+T` on fields.

#### Scenario: README documents flag and keys

- GIVEN the project README
- WHEN an operator reads connection-profile guidance
- THEN `save_connections` in `config.jsonc` and list key bindings are documented

#### Scenario: Status bar hints on connection screen

- GIVEN `save_connections` is true on the connection screen
- WHEN the operator views the status bar
- THEN hints include list navigation and `e` edit, `s` save-as, `r` rename, `d` delete, `t` test


### Requirement: Dump execution from connected session

The TUI MUST provide a dump flow through the existing dump runner. After source connection, the operator MUST select one or more source schemas on the **dump screen** before starting dump. Available choices MUST come from the source schema catalog loaded at connect. Dump MUST invoke `internal/dump` with session `*sql.DB` using only the selected schemas. No subprocess `dolly dump`. Dump MUST NOT run when no session, no output path, or no dump-screen schema selection is available. Dump MUST NOT depend on schema-screen picker state or a separate session-level schema gate.
(Previously: dump used `sessionSchemas` set at connect or schema-screen picker.)

#### Scenario: Dump with selected schemas

- GIVEN a connected source session and source schema catalog loaded
- WHEN the operator selects schemas on the dump screen and starts dump with a valid output path
- THEN dump runs using only the selected schemas
- AND progress or terminal status is shown without leaving the TUI

#### Scenario: Dump schema picker validates names

- GIVEN source schema catalog is loaded at connect
- WHEN the operator changes dump schema selection
- THEN only source-known schema names can be selected for dump

#### Scenario: Dump selection persists to profile

- GIVEN `save_connections` is true and an active saved profile
- WHEN the operator selects `app` and `billing` on the dump screen and starts dump
- THEN the profile `schemas` field is updated to `[app, billing]` before dump execution begins

#### Scenario: Dump blocked without connection

- GIVEN no session database
- WHEN dump is attempted
- THEN no dump runs and connect-first message appears

#### Scenario: Dump blocked with empty output path

- GIVEN empty output path
- WHEN dump is attempted
- THEN no dump runs and status bar hints to set path
### Requirement: Editable dump options

The dump screen MUST provide an editable output directory field and a transaction-mode toggle (`Transaction: on/off`) mapped to dump engine transaction options.

#### Scenario: Output path is editable

- GIVEN the dump screen is active
- WHEN the operator edits the output directory field
- THEN the entered path is used for the next dump run

#### Scenario: Transaction mode toggles

- GIVEN the dump screen is active
- WHEN the operator toggles transaction mode
- THEN the displayed label reflects on or off
- AND the next dump uses the corresponding engine option

### Requirement: Dump screen idle controls

On the idle dump screen (not running, not in result sub-mode), the operator MUST configure output path and transaction mode, select schemas, and scroll the log preview using section navigation. Starting a dump MUST require `Ctrl+Enter` when gates pass (session connected, non-empty output path, at least one schema selected). Plain `Enter` on overview MUST only drill into the highlighted section. Transaction toggle (`t`) MUST work when inside the Output directory section.
(Previously: `f` cycled path/schemas/log panes; `Enter` started dump.)

#### Scenario: Ctrl+Enter starts dump

- GIVEN a connected session, non-empty output directory, and at least one schema selected
- WHEN the operator presses `Ctrl+Enter` from dump overview
- THEN a dump run starts

#### Scenario: Enter on overview does not start dump

- GIVEN the same preconditions
- WHEN the operator presses `Enter` on overview with Output directory highlighted
- THEN the screen enters inside mode for Output directory
- AND no dump run starts

### Requirement: Async dump with cancellation

Dump execution MUST run asynchronously without blocking the TUI event loop. While running, duplicate start requests MUST be ignored. The operator MUST be able to cancel an in-flight dump; cancellation MUST return to idle with a status message.

#### Scenario: Dump runs without blocking UI

- GIVEN a dump is started
- WHEN export takes noticeable time
- THEN the TUI remains responsive to keyboard input except keys reserved for cancel
- AND the status bar shows a running hint

#### Scenario: Operator cancels running dump

- GIVEN a dump is in progress
- WHEN the operator cancels via the designated key
- THEN the dump context is cancelled
- AND the UI returns to idle with a cancellation message on the pane or status bar

#### Scenario: Quit cancels active dump

- GIVEN a dump is in progress
- WHEN the operator quits the application
- THEN the active dump is cancelled before the database session is closed

### Requirement: Table-granularity progress log

During a dump, the application MUST show a scrollable log fed by table-level progress events from the dump engine (table start/end only). Progress updates MUST NOT flood the event loop.

#### Scenario: Progress appears in log

- GIVEN a dump is running and the engine emits table-level progress events
- WHEN tables are processed
- THEN corresponding lines appear in the dump screen log
- AND the operator MAY scroll the log while running or after completion

### Requirement: Post-dump result summary sub-mode

After a dump reaches a terminal state (success or error, excluding cancellation), the dump screen MUST enter a **result sub-mode** replacing the dump pane body with a structured summary. The summary MUST show: an outcome banner (success/failure), the output directory path, generated artifact filenames (`.ndjson`, `metadata.json`), table count, and row **estimates** from `dump.ReadMetadata` when metadata exists. On failure, error text and any partial non-`.tmp` artifacts MUST appear when present. Row counts MUST be labeled as estimates. On 60×20 terminals the layout MUST remain readable via compact formatting and `j`/`k` scroll for long file lists. Summary data MUST come from `dump.ReadMetadata` and directory listing without changing the `dump.Dump` API.

While in result sub-mode, dump option editing and duplicate starts MUST be disabled. The operator MUST be able to: press `o` to open the output folder via an injectable `FolderOpener`; press `Enter` to dismiss and prepare another run; press `Esc` to dismiss to idle dump view; press `q` to quit. The status bar MUST show result-mode key hints.

#### Scenario: Success shows structured summary

- GIVEN a connected session, non-empty output path, and a successful dump with metadata
- WHEN `dump.Dump` completes without error
- THEN the dump screen enters result sub-mode
- AND the summary shows success banner, path, artifacts, table count, and row estimates

#### Scenario: Failure shows error and partial artifacts

- GIVEN a dump that fails after writing some artifacts
- WHEN `dump.Dump` returns a non-cancellation error
- THEN result sub-mode shows a failure banner and error text
- AND partial non-`.tmp` files are listed when present

#### Scenario: Cancel skips result sub-mode

- GIVEN a dump is cancelled by the operator
- WHEN cancellation completes
- THEN the dump screen returns to idle without entering result sub-mode

#### Scenario: Small terminal readability

- GIVEN terminal dimensions 60×20 and a result with multiple files
- WHEN result sub-mode is rendered
- THEN content fits via compact layout and scroll without crashing

#### Scenario: Open folder via injectable opener

- GIVEN result sub-mode is active and tests inject a mock `FolderOpener`
- WHEN the operator presses `o`
- THEN the opener is invoked with the output path
- AND default tests require no subprocess or desktop integration

#### Scenario: Result dismiss and run again

- GIVEN result sub-mode is active after success
- WHEN the operator presses `Enter`
- THEN result sub-mode dismisses and the dump screen is ready for another run

#### Scenario: Esc dismisses result

- GIVEN result sub-mode is active
- WHEN the operator presses `Esc`
- THEN result sub-mode dismisses to idle dump view

### Requirement: Safe dump error handling

Dump failures MUST surface descriptive errors and MUST enter result sub-mode when the dump reaches a terminal error state (not cancellation). Errors MUST NOT panic or terminate the TUI.

#### Scenario: Dump engine error

- GIVEN a connected session and configured output path
- WHEN `dump.Dump` returns an error other than cancellation
- THEN result sub-mode shows the error with a failure banner
- AND the application remains running on the dump screen

### Requirement: Testable dump runner seam

The TUI MUST expose an injectable dump-runner abstraction so tests exercise success, failure, cancellation, and gated runs without live PostgreSQL.

#### Scenario: Unit tests without live database for dump

- GIVEN tests inject a fake dump runner
- WHEN dump messages are exercised
- THEN success, failure, cancel, no-session, and empty-path cases are asserted
- AND default `go test ./internal/tui/...` requires no PostgreSQL

### Requirement: Clone execution with schema picker

The TUI MUST provide a clone flow through the controlled `clonework` runner. After source connection, the operator MUST select one or more source schemas on the **clone screen** before starting clone. Available choices MUST come from the source schema catalog loaded at connect. Target DSN MUST offer Manual, Saved profile, and Current DSN; Current DSN MUST be default. Clone name MUST prefill from config and increment `{n}` until non-conflicting. When the active profile has non-empty `schemas`, those names MUST be pre-selected in the clone picker as defaults. Clone MAY execute `pg_dump` and `psql` through that runner to use the clone execution layer's `pg_dump --schema-only | psql` pipeline. Clone MUST NOT run when no session, no target input, or no clone-screen schema selection is available. Clone MUST NOT depend on dump-screen selection or schema-screen picker state.
(Previously: clone reused `sessionSchemas` populated by the schema-screen picker or profile at connect; no target picker, Current DSN default, or auto-name.)

#### Scenario: Clone with selected schemas

- GIVEN a connected source session and source schema catalog loaded
- WHEN the operator selects schemas on the clone screen and starts clone with valid target input
- THEN clone runs through `clonework` using only the selected schemas
- AND progress or terminal status is shown without leaving the TUI

#### Scenario: Current DSN default

- GIVEN source connect succeeded
- WHEN clone opens
- THEN Target DSN is populated from Current DSN

#### Scenario: Saved target profile

- GIVEN saved profiles exist
- WHEN one is selected as target
- THEN target uses it without showing password

#### Scenario: Manual target entry

- GIVEN the target picker is open
- WHEN Manual is selected
- THEN target DSN is editable

#### Scenario: Auto clone name

- GIVEN template `{db}_dolly_{n}` and database `app`
- WHEN clone opens
- THEN first free `app_dolly_{n}` name is shown

#### Scenario: Clone blocked without schemas

- GIVEN clone screen is active with no schemas selected in the clone picker
- WHEN the operator starts clone
- THEN no clone execution occurs
- AND the status bar asks the operator to select schemas on the clone screen

#### Scenario: Clone schema picker validates names

- GIVEN source schema catalog is loaded at connect
- WHEN the operator changes clone schema selection
- THEN only source-known schema names can be selected for clone

#### Scenario: Profile schemas pre-select clone picker

- GIVEN profile `schemas: [app, billing]` and successful connect
- WHEN the operator opens the clone screen
- THEN `app` and `billing` are pre-selected in the clone picker
- AND the operator may change selection before starting clone

#### Scenario: Clone selection persists to profile

- GIVEN `save_connections` is true and an active saved profile
- WHEN the operator selects `public` and `app` on the clone screen and starts clone
- THEN the profile `schemas` field is updated to `[app, public]` (sorted) before clone execution begins

### Requirement: Clone screen idle controls

On the idle clone screen (not running, not in result sub-mode), the operator MUST edit clone fields, use the Target DSN picker, select schemas, and scroll the log preview using section navigation. Starting a clone MUST require `Ctrl+Enter` when gates pass. Plain `Enter` on overview MUST only drill into the highlighted section. The operator MUST cycle clone strategy with `←`/`→`, toggle optional analyze preflight with `a`, and when analyze is enabled see async table count plus database size before clone execution.
(Previously: `f` cycled form/schemas/log; `Enter` started clone; covered fields, schemas, log, and `Ctrl+Enter` only.)

#### Scenario: Ctrl+Enter starts clone

- GIVEN a connected session, non-empty target DSN, and at least one schema selected
- WHEN the operator presses `Ctrl+Enter`
- THEN a clone run starts

#### Scenario: Strategy cycles

- GIVEN clone target section is active
- WHEN `←` or `→` is pressed
- THEN another allowed strategy is visible

#### Scenario: Analyze preflight

- GIVEN analyze is enabled and gates pass
- WHEN clone starts
- THEN table count and database size appear before execution

#### Scenario: Analyze cancel

- GIVEN analyze is running
- WHEN `Esc` is pressed
- THEN analyze cancels and clone does not start

### Requirement: Testable clone runner seam

The TUI MUST expose and preserve injectable seams so tests cover clone results, gates, target sources, auto-name, strategy, analyze, and goldens without live PostgreSQL or real `pg_dump`/`psql` execution.
(Previously: the seam was required for in-process clone testing only; tests covered runner outcomes and basic gates only.)

#### Scenario: Unit tests without live database for clone

- GIVEN tests inject a fake clone runner
- WHEN clone messages are exercised
- THEN success, failure, cancel, no-session, no-target, and no-schema cases are asserted

#### Scenario: Unit clone tests

- GIVEN fake clone, store, and analyze seams
- WHEN clone messages run
- THEN success, failure, cancel, gates, picker, strategy, and analyze are asserted

#### Scenario: Clone goldens

- GIVEN clone goldens run
- WHEN views render
- THEN picker, auto-name, strategy, and analyze states match expected output

### Requirement: Two-level section navigation

Screens with multiple vertical sections (Connection when saved profiles are enabled, Dump, Clone) MUST use a two-level focus model: **overview** and **inside**. In overview, the operator MUST move among section rows with `↑`/`↓` (and `j`/`k` where already supported). `Enter` on the highlighted section MUST enter that section (inside level). `Esc` while inside MUST return to overview without quitting the application. While inside a section, `Tab` and `Shift+Tab` MUST move focus among focusable items within that section (form fields, schema picker, or log scroll target as applicable). The UI MUST render section rows in vertical order with a visible overview cursor (e.g. `>` prefix) on the active section in overview mode. Overview MUST show a compact vertical list of sections (not expanded pane bodies). Inside MUST show only the active section body.
(Previously: overview still rendered all section bodies stacked; sections were focused by cycling with `f` / `Shift+f`; `↑`/`↓` only affected the already-focused pane.)

#### Scenario: Overview section cursor moves vertically

- GIVEN the dump screen is idle and in overview mode
- WHEN the operator presses `↓` repeatedly
- THEN the section highlight moves through Output directory, Schemas, and Log in order
- AND plain `Enter` does not start a dump run

#### Scenario: Enter drills into a section

- GIVEN the dump screen overview with Schemas highlighted
- WHEN the operator presses `Enter`
- THEN the screen is in inside mode for Schemas
- AND `Space` toggles schema selection without pressing `f` first

#### Scenario: Esc returns to overview

- GIVEN the clone screen inside the Target & strategy section
- WHEN the operator presses `Esc`
- THEN the screen returns to overview mode
- AND the overview cursor remains on Target & strategy

#### Scenario: f no longer cycles panes

- GIVEN an idle dump, clone, or connection screen (fields/list panels)
- WHEN the operator presses `f`
- THEN no section or screen change occurs

### Requirement: Collapsed section overview

On dump, clone, and connection (when saved profiles enabled), overview mode MUST render only section row labels with optional summaries. Full section content (fields, picker, log) MUST appear only in inside mode after `Enter`.

#### Scenario: Dump overview is collapsed

- GIVEN the dump screen is idle in overview
- WHEN the operator views the body
- THEN only Output directory, Schemas, and Log rows are visible
- AND the schema picker list is not expanded

#### Scenario: Esc returns from inside to overview

- GIVEN connection inside the fields section without saved profiles
- WHEN the operator presses `Esc`
- THEN the screen returns to section overview
- AND a second `Esc` opens the quit confirmation

### Requirement: Vertical workflow screen menu

The application MUST render workflow screens (connection, schema, dump, clone, config) in a vertical menu in a left column. The active screen MUST be indicated with `>` and accent styling. Digit keys `1`–`5` MUST remain bound to the same screens. The UI MUST NOT render a horizontal breadcrumb (`connection › schema › …`) above the main body.

#### Scenario: Vertical menu shows active screen

- GIVEN the dump screen is active
- WHEN the operator views the layout
- THEN a left column lists `1 connect` through `5 config` vertically
- AND `3 dump` is highlighted as active

### Requirement: Screen navigation and help

The operator MUST navigate workflow screens with digit shortcuts `1` (connection), `2` (schema), `3` (dump), `4` (clone), and `5` (config). `Ctrl+Tab` and `Ctrl+Shift+Tab` MUST cycle forward and backward among workflow screens. Plain `Tab` and `Shift+Tab` MUST NOT change workflow screens. Digit shortcuts MUST be ignored while the operator types in a text field, profile name prompt, or connection edit panel (deferral). Digit and `Ctrl+Tab` screen jumps MUST land the target screen in section overview (not inside), regardless of `tui.section_entry`. App initialization MUST honor `tui.section_entry` from config. Help (`?`) MUST be split-view, document digit screen jumps, section overview keys (`↑`/`↓`, `Enter`, `Esc`), in-section `Tab`, and `Ctrl+Enter` for dump/clone run without masking borders.
(Previously: Tab/Shift+Tab cycled screens globally; Connection used Tab for fields/list when save-as/rename/edit submodes were inactive; `f` cycled subpanes on dump/clone/connection.)

#### Scenario: Direct screen selection by digit

- GIVEN an active session on the schema screen
- WHEN the operator presses `4`
- THEN the clone screen activates

#### Scenario: Plain Tab does not change screen

- GIVEN the dump screen is idle
- WHEN the operator presses `Tab` without Ctrl
- THEN the screen remains dump
- AND focus moves within the current section if inside, or Tab is a no-op on overview

#### Scenario: Ctrl+Tab still cycles screens

- GIVEN the connection screen is active
- WHEN the operator presses `Ctrl+Tab`
- THEN the schema screen activates

#### Scenario: Digit deferred while editing host

- GIVEN the connection fields section is inside mode with Host focused
- WHEN the operator types `192.168.1.5`
- THEN the screen does not jump to config
- AND the host field contains the typed characters

#### Scenario: Digit jump opens dump overview

- GIVEN the schema screen is active
- WHEN the operator presses `3`
- THEN the dump screen activates in section overview
- AND output path field is not in edit mode until Enter

#### Scenario: Help opens as split view

- GIVEN any workflow screen
- WHEN pressing `?`
- THEN bindings and CLI catalog appear beside primary content

### Requirement: Global keyboard help overlay

Help (`?`) MUST list context bindings plus CLI catalog. Connection help MUST document section overview, `Ctrl+T` (fields test), `t` (list test), and letter profile actions. Dump and clone help MUST document section overview, `Ctrl+Enter` run, and MUST NOT document `f` for pane cycling.
(Previously: dump/clone help documented `f` / `Shift+f` pane keys; connection help documented panel `Tab`.)

#### Scenario: Help opens with CLI catalog

- GIVEN any workflow screen
- WHEN pressing `?`
- THEN bindings and CLI flag tables appear

#### Scenario: Connection help documents saved profiles and edit

- GIVEN `save_connections` is true on connection screen
- WHEN help opens
- THEN list keys including `e`/`s`/`r`/`d`/`t` and section overview keys are documented
- AND function keys for profile actions are not listed

#### Scenario: Dump help documents section nav

- GIVEN the dump screen
- WHEN help opens
- THEN bindings include `↑`/`↓` section, `Enter` open, `Esc` back, and `Ctrl+Enter` dump
- AND `f` is not listed as a pane key

### Requirement: Persistent status bar

Every screen MUST render a status bar with at most five hint items, including connecting, test-connection, and error states during database operations. On dump and clone idle screens in overview, the status bar MUST mention section navigation and `Ctrl+Enter` to run. On inside mode, the status bar MUST mention `Esc` back and section-relevant keys. Connection with saved profiles MUST NOT mention `f` for panel switching.
(Previously: dump/clone status included `f panes`; connection with profiles included `f/Tab`.)

#### Scenario: Status bar on all screens

- GIVEN any workflow screen is active
- WHEN the operator views the screen
- THEN a status bar is visible at the bottom
- AND it includes hints relevant to the active screen or global actions

#### Scenario: Status bar with help overlay

- GIVEN the help overlay is visible
- WHEN the operator views the display
- THEN the status bar remains visible or its hints remain accessible alongside the overlay

#### Scenario: Status bar during connection

- GIVEN an async connect is in progress
- WHEN the operator views the connection screen
- THEN the status bar shows a connecting hint

#### Scenario: Status bar during test connection

- GIVEN an async ping-only test is in progress or has just completed on the connection screen
- WHEN the operator views the connection screen
- THEN the status bar shows an in-flight hint while testing
- AND on success shows reachable feedback on the status bar only

#### Scenario: Dump overview status hint

- GIVEN the dump screen idle in overview
- WHEN the status bar renders with no override message
- THEN it includes section and run hints using at most five segments
- AND it does not mention `f`

### Requirement: Status, progress, and verification

Every screen MUST render a status bar with at most five hint items. Connect, dump, and clone in-flight states MUST show progress feedback. Tests MUST cover Connection-first launch, letter actions, modal outcomes, cursor behavior, fixed mask, spinner states, compact status/help, and goldens without live DB or TTY.
(Previously: status could exceed five items; progress feedback and UX-standard tests were not required.)

#### Scenario: Compact status bar

- GIVEN any screen is active
- WHEN it renders
- THEN status has at most five hint items

#### Scenario: Progress feedback

- GIVEN an operation is in progress
- WHEN the active screen renders
- THEN spinner or equivalent progress is visible

#### Scenario: Behavioral tests pass

- GIVEN `go test ./internal/tui/...`
- WHEN tests run
- THEN UX state transitions and goldens are verified

### Requirement: Persistent CLI capabilities strip

The application MUST render a compact **capabilities strip** on every workflow screen (help closed). It MUST name `dump`, `restore`, `clone`, and `tui` with one-line purposes; label `restore` as available via **CLI and TUI history restore**; present `clone` as available in the TUI when the clone flow is supported; note subset dump flags (`--seed-file`, `--max-*`) as CLI-only. Strip sits above the status bar (visible on 80×24). At 60×20: one truncated line (`… +N flags`); full flags in `?` only.

#### Scenario: Strip on every screen

- GIVEN any workflow screen
- WHEN the operator views the screen
- THEN the strip shows all four commands with purpose text

#### Scenario: 60×20 compact strip

- GIVEN dimensions 60×20
- WHEN a workflow screen renders
- THEN the strip is one line, status bar visible, layout stable

#### Scenario: 80×24 expanded strip

- GIVEN dimensions 80×24
- WHEN a workflow screen renders
- THEN expanded summaries appear without opening `?`
- AND the status bar remains below the strip

### Requirement: CLI capability catalog and flag parity

One catalog MUST align with `parseDumpFlags`, `parseRestoreFlags`, and `parseCloneFlags`, including `--connection` when parsers support it.
(Previously: catalog omitted `--connection`.)

#### Scenario: Parity test catches drift

- GIVEN a parser flag added without catalog update
- WHEN `go test ./internal/tui/...` runs
- THEN parity tests fail
### Requirement: Shell-only discovery labels

`restore` CLI and subset dump surfacing MUST remain documentary on the capabilities strip (no ad-hoc subprocess restore from discovery UI). The dump screen MAY execute restore for a selected history entry via `restore_run.go`. `clone` MUST NOT be labeled shell-only once the TUI clone flow is available.

#### Scenario: Non-executable labeling for subset dump

- GIVEN strip or help shows subset dump options
- WHEN the operator reads it
- THEN text states subset dump is CLI-only

#### Scenario: Restore labeling

- GIVEN strip or help shows restore
- WHEN the operator reads it
- THEN text states restore is available via CLI (`--input`) and TUI dump history

#### Scenario: Clone is executable from TUI

- GIVEN strip or help shows clone
- WHEN the operator reads it
- THEN text indicates clone can be started from the TUI clone flow

### Requirement: Dense operator visual theme

The application MUST use a consistent dense terminal aesthetic suited to operator workflows: monospace typography, tight spacing, muted borders, a title in the body header, and a left vertical workflow screen menu (not a horizontal breadcrumb).

#### Scenario: Consistent styling across screens

- GIVEN the operator navigates among all five workflow screens (Connection, Schema, Dump, Clone, Config)
- WHEN each screen is rendered
- THEN typography, spacing, and border treatment remain consistent
- AND each screen shows title context in the body with the vertical menu indicating the active step

### Requirement: Config screen view and edit

The TUI MUST provide a Config workflow screen (screen 5, key `5`) displaying all 24 configuration knobs as scrollable rows grouped by section. The operator MUST move between rows with `↑`/`↓` or `j`/`k`. The operator MUST enter edit mode for the focused string, bool, or int/duration knob and confirm with Enter. Array-type fields (e.g. `schemas`) MUST be displayed read-only. After editing, the operator MUST save to the active config path via a designated key (`Ctrl+S`); saving MUST call `SaveConfig`. The status bar MUST show config-screen hints (save, cancel, scroll). Help (`?`) MUST document config screen bindings.

#### Scenario: Config screen shows all knobs

- GIVEN the operator presses `5` to navigate to Config
- WHEN the screen renders
- THEN all 24 knobs are shown with current resolved values grouped by section
- AND the screen is scrollable on a 60×20 terminal

#### Scenario: Edit a string knob

- GIVEN the operator focuses a string knob and presses Enter to enter edit mode
- WHEN the operator types a new value and confirms with Enter
- THEN the in-memory config reflects the new value

#### Scenario: Save writes to disk

- GIVEN at least one knob has been edited
- WHEN the operator presses `Ctrl+S`
- THEN `SaveConfig` is called with the updated config and config file path
- AND the status bar confirms success or shows an error

#### Scenario: Array fields are read-only

- GIVEN the operator focuses an array-type knob
- WHEN the operator attempts to edit it
- THEN no edit mode activates and a read-only hint appears on the status bar

#### Scenario: Config screen layout on 60×20

- GIVEN terminal dimensions 60×20
- WHEN Config screen renders
- THEN content is scrollable and layout is stable without overflow or crash

### Requirement: Data-layer isolation

Execution rules: `internal/dump` MAY be used for in-TUI dump. Clone execution MUST be routed only through `clonework`; TUI code MUST NOT import `internal/clone`, `os/exec`, `internal/restore`, or `internal/dump` for clone execution. Clone MAY execute `pg_dump`/`psql` through the controlled runner. Full restore via CLI (`dolly restore --input`) remains CLI-only; the dump screen MAY restore a previously selected numbered dump directory from history through `restore_run.go` (same `internal/restore` seam as CLI, not subprocess). Documentary surfacing of restore and CLI-only dump flags via strip/help IS permitted. Database introspection via `internal/db` IS permitted.
(Previously: clone subprocesses were prohibited and clone import could use the clone execution layer directly.)

#### Scenario: Dump import allowed

- GIVEN TUI package build
- WHEN dependencies are inspected
- THEN `internal/dump` MAY be used for dump screen
- AND `internal/restore` is imported only from `restore_run.go`

#### Scenario: TUI history restore allowed

- GIVEN dump history lists a completed dump directory
- WHEN the operator restores from the dump screen history section
- THEN restore executes through `restore_run.go` against the connected session
- AND no shell subprocess is spawned for restore

#### Scenario: Clone execution allowed through controlled runner

- GIVEN TUI clone flow is implemented
- WHEN dependencies and subprocess use are inspected
- THEN TUI clone execution imports `clonework` only
- AND any `pg_dump`/`psql` execution occurs through the controlled clone runner

#### Scenario: Database access allowed

- GIVEN a successful connection workflow
- WHEN the operator connects and loads schema
- THEN introspection uses `internal/db` for schema selection
### Requirement: Connection keyboard handling when saved profiles enabled

When `save_connections` is true, the connection screen MUST present Connection fields and Saved profiles as overview sections. `Enter` on Saved profiles MUST enter inside mode for the list. Inside the fields section, `Tab` MUST cycle field focus; `Enter` MUST connect; `Ctrl+T` MUST ping. Inside the saved list, `↑`/`↓` MUST move selection; letter actions `e`/`s`/`r`/`d`/`t` MUST apply to the selected profile; `Enter` MUST pick and connect. `f` MUST NOT toggle between fields and list. Edit panel and name prompts MUST follow **Saved-profile actions** letter-key bindings; edit panel Enter saves via `Put`; Esc cancels.
(Previously: `f` toggled fields/list panels; Tab could advance screens globally; `Tab` switched panels directly.)

#### Scenario: Enter opens saved list section

- GIVEN `save_connections` is true and connection overview with Saved profiles highlighted
- WHEN the operator presses `Enter`
- THEN inside mode activates for the saved list
- AND `↑`/`↓` move the list selection

#### Scenario: Tab on connection does not jump to schema

- GIVEN connection inside fields section
- WHEN the operator presses `Tab`
- THEN focus moves to the next connection field
- AND the active screen remains connection

#### Scenario: Edit shortcut on list

- GIVEN the saved list panel is active
- WHEN pressing `e`
- THEN the field-edit panel opens for the selected profile

#### Scenario: List letter shortcuts

- GIVEN the saved list panel is active
- WHEN pressing `s`, `r`, or `d`
- THEN save-as, rename, or delete runs without typing into fields

### Requirement: Automated behavioral verification

Tests MUST cover catalog parity, strip goldens at 60×20 and 80×24, connection/dump/result flows, and when `save_connections` is enabled: list (empty/non-empty), save-as, auto-save, rename, delete, and field-edit-save goldens at both sizes—without live DB or TTY.
(Previously: no saved-list or edit-save coverage.)

#### Scenario: Update tests pass

- GIVEN `go test ./internal/tui/...`
- WHEN logic tests run
- THEN saved-profile and edit-save flows are asserted

#### Scenario: Render goldens pass

- GIVEN 60×20 and 80×24
- WHEN view goldens run
- THEN connection list and edit-panel views match expected output

### Requirement: Destructive history restore confirmation

Before starting a restore from the dump screen History section, the TUI MUST evaluate `clone.replace` and `clone.restore_on_conflict` from the loaded config. When `clone.replace` is true OR `restore_on_conflict` is `upsert`, the TUI MUST show a Y/N confirmation modal describing the destructive policy, the selected dump path, and the redacted target database connection for the active session (passwords and other secrets MUST NOT appear in plaintext). Enter/r on History MUST NOT start restore until the operator confirms with `Y`. `N` or Esc MUST dismiss without side effects. When policy is `error` or `skip` and `replace` is false, restore MUST proceed without a confirmation modal (existing Enter/r behavior).
(Previously: confirmation modal described destructive policy and dump path only; target connection was omitted.)

#### Scenario: Replace enabled requires confirmation

- GIVEN `clone.replace` is true and a history entry is selected
- WHEN the operator presses Enter or `r` in History
- THEN a confirmation modal appears mentioning truncate/replace
- AND restore starts only after `Y`

#### Scenario: Upsert policy requires confirmation

- GIVEN `clone.restore_on_conflict` is `upsert` and replace is false
- WHEN the operator confirms restore from History
- THEN a confirmation modal appears before restore runs

#### Scenario: Error/skip policy restores immediately

- GIVEN `clone.replace` is false and `restore_on_conflict` is `error` or `skip`
- WHEN the operator presses Enter on a history entry
- THEN restore starts without a confirmation modal

#### Scenario: Confirmation shows redacted target connection

- GIVEN a destructive-restore confirmation modal is shown
- AND the active session target DSN contains a password or other secret
- WHEN the operator reads the modal body
- THEN the redacted target connection is visible alongside the dump path
- AND no password or secret appears in plaintext

#### Scenario: Confirmation target matches session database

- GIVEN a connected session with a known target database
- WHEN a destructive-restore confirmation modal is shown for a selected history entry
- THEN the modal body identifies the same target database as the active session connection
- AND the dump path shown matches the selected history entry

### Requirement: Progress bar with ETA

During a running dump, restore-from-history, or clone, the TUI SHALL render a progress bar with percentage and ETA in addition to the existing log or status feedback. Progress updates MUST be table- or step-granularity and MUST NOT flood the Bubble Tea update loop. ETA MUST be omitted or marked pending until enough progress exists to avoid divide-by-zero or negative values.

#### Scenario: Running operation shows bar and log

- GIVEN dump, restore, or clone is running and progress events are available
- WHEN the active workflow screen renders
- THEN it shows a progress bar with percentage and ETA when computable
- AND existing log/status feedback remains visible

#### Scenario: Early progress avoids bad ETA

- GIVEN an operation has no event or only the first progress event
- WHEN the screen renders progress
- THEN no divide-by-zero or negative ETA is shown
- AND the layout remains stable

#### Scenario: Progress updates are bounded

- GIVEN many progress events arrive quickly
- WHEN the TUI processes updates
- THEN rendering uses the latest progress state without unbounded message flooding
