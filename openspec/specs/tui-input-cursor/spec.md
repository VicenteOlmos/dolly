# tui-input-cursor Specification

## Purpose

Define shared cursor movement behavior for editable TUI text fields without adopting a new input widget.

## Requirements

### Requirement: In-field cursor navigation

Every editable TUI field MUST support left/right cursor movement, insertion at cursor, deletion before cursor, and deletion at cursor. Cursor keys MUST affect only the focused field while text entry is active.

#### Scenario: Insert at cursor

- GIVEN an editable field contains `ac` with cursor between `a` and `c`
- WHEN the operator types `b`
- THEN the field value becomes `abc`

#### Scenario: Move within field

- GIVEN an editable field is focused
- WHEN the operator presses left or right arrow
- THEN the cursor moves within field bounds

#### Scenario: Global navigation is not triggered

- GIVEN a text field is focused
- WHEN the operator presses cursor-navigation keys
- THEN workflow screen navigation does not run

### Requirement: Field focus and masking compatibility

Cursor behavior MUST work for connection, edit-profile, dump path, clone target, and prompt fields. Password display MUST remain fixed-length masked while preserving editable underlying text.

#### Scenario: Password mask stays fixed

- GIVEN password text is non-empty
- WHEN the operator moves the cursor or edits the password
- THEN the rendered password remains exactly eight `*`

#### Scenario: Empty password remains empty

- GIVEN password text is empty
- WHEN the password field renders
- THEN no mask characters are shown
