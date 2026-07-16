# Project README Specification

## Purpose

Define the English `README.md` as a scannable, evidence-backed product homepage for developers and operators, without changing Dolly behavior.

## Requirements

### Requirement: Reader Journey and Path Selection

The README MUST lead readers from Dolly's local-first PostgreSQL value and proof to a clear choice between TUI and CLI, installation, and a first workflow. It SHOULD use progressive disclosure: value and path selection first, then installation, first use, capabilities, safety and limits, configuration/automation, and development/release references.

#### Scenario: New reader reaches first workflow

- GIVEN a reader opens the English README without prior Dolly context
- WHEN the reader follows opening sections
- THEN the reader can choose TUI or CLI, install Dolly, and reach a first workflow

#### Scenario: Reader scans documentation

- GIVEN a reader needs one operational path
- WHEN the reader scans headings, tables, commands, and links
- THEN Markdown structure exposes relevant action without requiring recall of hidden context

### Requirement: Evidence and Scope Boundaries

Every example and claim MUST be traceable to repository evidence, including installer scripts, CLI help/tests, security documentation, release evidence, and configuration examples. The README MUST NOT include screenshots, badges, benchmarks, unsupported claims, attribution, or unrelated assets, and MUST NOT imply code, installer, CLI, release, publication, or Spanish README changes.

#### Scenario: Claim is published

- GIVEN proposed README copy contains a product claim or command example
- WHEN it is reviewed against repository evidence
- THEN it is retained only when evidence supports it

#### Scenario: Unsupported visual proof is proposed

- GIVEN a screenshot or unverifiable claim would improve presentation
- WHEN scope is checked
- THEN it is omitted

### Requirement: Installation and Verification Contracts

The README MUST preserve copyable and accurate Linux/macOS, Windows, and source-build installation guidance, version pinning, checksum verification, fail-closed behavior, and the exact override `DOLLY_ALLOW_UNVERIFIED=1`. It MUST preserve operational safety wording rather than weakening or paraphrasing commands into ambiguous instructions.

#### Scenario: Platform installation is selected

- GIVEN a reader selects Linux/macOS or Windows
- WHEN the reader copies installation instructions
- THEN the corresponding installer, pinning, checksum, and failure behavior remain explicit and accurate

#### Scenario: Verification cannot complete

- GIVEN checksum verification is unavailable or fails
- WHEN the reader follows README guidance
- THEN fail-closed behavior is clear and the exact opt-in `DOLLY_ALLOW_UNVERIFIED=1` override is shown

### Requirement: TUI, CLI, Safety, Limits, and Failure Accessibility

The README MUST describe both interactive TUI and scriptable CLI paths, preserve documented safety and limits, and present commands, warnings, links, and failure guidance accessibly. Copy MUST remain usable in narrow layouts and MUST identify failure outcomes without claiming runtime behavior beyond evidence.

#### Scenario: Operator chooses automation

- GIVEN an operator wants repeatable or scriptable use
- WHEN the operator reads path and first-workflow sections
- THEN CLI commands and their documented safety boundaries are available

#### Scenario: Reader encounters a failure or warning

- GIVEN installation, verification, or workflow can fail
- WHEN the reader consults README guidance
- THEN failure behavior, limits, and safe next action are visible and unambiguous

### Requirement: Immutable Page-Context Hero Acceptance

The verifier MUST run native preflight before browser work and perform exactly one fresh browser call. The call MUST receive raw `sourceOuterHTML` as a serializable argument to `page.evaluate`; every `new DOMParser()` MUST occur lexically inside that callback, and all DOM-derived metrics MUST derive only from that raw argument. `DOMParser` MUST NOT be used in server context. Encoded markup MAY be used only as `img.src` data-URI transport.

The call MUST produce six exact-width-first panels for `1012`, `375`, and `320` pixels in light and dark modes, retaining all existing geometry, readability, clipping, overlap, contrast, and alpha metrics, plus exactly one screenshot grid. Cleanup and postflight MUST prove immutable pre/post equality. The verifier MUST NOT retry, mutate targets or state, or deliver/publish artifacts.

#### Scenario: Native preflight and single browser call pass

- GIVEN approved immutable staged targets and receipt/state
- WHEN native preflight passes and verification runs
- THEN exactly one browser call produces six panels and one screenshot grid
- AND postflight manifests equal preflight manifests

#### Scenario: Metrics stay in page context

- GIVEN raw SVG `sourceOuterHTML` is passed to `page.evaluate`
- WHEN the callback computes panel metrics
- THEN every `new DOMParser()` is lexically inside the callback and metrics derive only from the raw argument
- AND no server-context parser or non-`img.src` data URI is used

#### Scenario: Any forbidden condition fails closed

- GIVEN preflight, browser, cleanup, or equality check fails
- WHEN the verifier records the result
- THEN it records FAIL without retry, mutation, extra call, or delivery

### Requirement: Optional Hero and Change Limits

The redesign MAY add only `assets/readme/hero.svg`. If added, it MUST pass GitHub-style preview checks for narrow-layout legibility, light/dark contrast, meaningful alt text, and factual content; otherwise it MUST be omitted. The change MUST modify no more than two files and remain within 800 changed lines. Final acceptance MUST use the immutable page-context verification contract above.

#### Scenario: Hero passes validation

- GIVEN the optional hero is proposed
- WHEN narrow and light/dark previews confirm all acceptance conditions
- THEN the hero MAY be included with meaningful alt text

#### Scenario: Hero fails validation or scope grows

- GIVEN any hero acceptance check fails, or proposed changes exceed two files or 800 lines
- WHEN the change is reviewed
- THEN the hero is omitted or scope is reduced before acceptance
