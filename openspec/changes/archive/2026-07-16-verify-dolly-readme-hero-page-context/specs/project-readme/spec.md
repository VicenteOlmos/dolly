# Delta for Project README

## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Optional Hero and Change Limits

The redesign MAY add only `assets/readme/hero.svg`. If added, it MUST pass GitHub-style preview checks for narrow-layout legibility, light/dark contrast, meaningful alt text, and factual content; otherwise it MUST be omitted. The change MUST modify no more than two files and remain within 800 changed lines. Final acceptance MUST use the immutable page-context verification contract above.
(Previously: Hero acceptance required preview checks but did not specify the corrected browser execution context.)

#### Scenario: Hero passes validation

- GIVEN the optional hero is proposed
- WHEN native preflight passes and one page-context call confirms all six exact-width-first panels, existing metrics, and one screenshot grid
- THEN the hero MAY be included with meaningful alt text

#### Scenario: Hero fails validation or scope grows

- GIVEN any hero acceptance check fails, or proposed changes exceed two files or 800 lines
- WHEN the change is reviewed
- THEN the hero is omitted or scope is reduced before acceptance
- AND no retry, repair, mutation, or publication occurs
