# Delta for dolly-tui

## MODIFIED Requirements

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
