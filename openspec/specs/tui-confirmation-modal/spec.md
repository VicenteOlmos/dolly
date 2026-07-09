# tui-confirmation-modal Specification

## Purpose

Provide a reusable TUI confirmation contract for destructive or terminal actions.

## Requirements

### Requirement: Modal confirmation gate

The system MUST show a focused confirmation modal before destructive saved-profile actions or terminal quit/cancel actions that can discard work. While open, the modal MUST intercept keys and return exactly one outcome: confirm, cancel, or still pending.

#### Scenario: Confirm destructive action

- GIVEN a profile delete confirmation is open
- WHEN the operator accepts the modal
- THEN the pending delete action is allowed to continue

#### Scenario: Cancel destructive action

- GIVEN a profile delete confirmation is open
- WHEN the operator cancels or presses `Esc`
- THEN no delete action runs

#### Scenario: Background keys are blocked

- GIVEN a confirmation modal is open
- WHEN the operator presses a workflow or list-action key
- THEN the underlying screen state does not change

### Requirement: Modal copy and focus clarity

The modal MUST identify the action, consequence, confirm key, and cancel key without hiding the persistent status context.

#### Scenario: Modal is understandable

- GIVEN a confirmation modal is rendered
- WHEN the operator reads it
- THEN action, consequence, confirm, and cancel choices are visible

#### Scenario: Small terminal remains stable

- GIVEN a 60×20 terminal
- WHEN the modal is visible
- THEN layout remains readable without crashing
