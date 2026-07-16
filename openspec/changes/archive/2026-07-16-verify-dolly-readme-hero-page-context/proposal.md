# Proposal: Verify Dolly README Hero in Page Context

## Intent

Use the authorized fresh browser call to correct the prior verifier context error (`ReferenceError: DOMParser is not defined`) and judge the unchanged staged README hero. Preserve product, authority, receipt, prior-artifact, repository, and publication state.

## Proposal Question Round

Launch constraints resolve scope, invariants, failure behavior, and tradeoffs; no assumptions remain open.

## Scope

### In Scope
- Run cached automatic preflight through native commands, then repository checks and immutable pre/post comparison.
- Make exactly one fresh browser call producing six exact-width-first panels (`1012`, `375`, `320`; light and dark) and one screenshot grid.
- Construct and use `DOMParser` only inside `page.evaluate(({sourceOuterHTML, ...}) => { ... })`; pass raw `sourceOuterHTML` as a serializable argument and use it as the sole metric source.
- Use a data URI only as `img.src` transport.

### Out of Scope
- Product, README, SVG, authority, receipt/state, prior-artifact, ref, or publication changes.
- Retry, repair, extra browser calls, generated harnesses, commit, push, PR creation, or publication.

## Capabilities

### New Capabilities
None.

### Modified Capabilities
- `project-readme`: Correct final immutable browser acceptance so SVG parsing occurs exclusively in browser page context.

## Approach

Preserve the predecessor contract; change only execution context. Fail closed on preflight mismatch. In the sole call, pass raw markup into `page.evaluate`; create every `DOMParser` there, derive every SVG metric from that argument, and isolate encoded transport to `img.src`. Assert six direct content-box widths first, capture one grid, clean browser state once, run native repository checks, and require equal manifests. Any failure records FAIL without retry or mutation.

## Affected Areas

| Area | Impact | Description |
|---|---|---|
| `openspec/changes/verify-dolly-readme-hero-page-context/` | New | Acceptance contract and later evidence |
| `README.md`, `assets/readme/hero.svg` | Validated | Immutable staged targets |
| `openspec/changes/verify-dolly-readme-hero-final-browser/` | Read-only | Immutable failure lineage |

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Page/server context leakage | Medium | `DOMParser` appears only inside evaluate callback |
| Sole call or equality check fails | Medium | Record FAIL; no retry or repair |

## Rollback Plan

No product rollback exists. On failure, retain the immutable report and remove only incomplete new evidence; preserve all inputs and prior artifacts.

## Dependencies

- Authorized one-call budget, approved receipt/state, browser runtime, cached automatic preflight, single-PR delivery within 800 lines.

## Success Criteria

- [ ] Exactly one fresh browser call completes six exact-width-first cases and one hash-bound grid.
- [ ] All parsers run inside `page.evaluate`; raw `sourceOuterHTML` is the sole metric source and data URI only supplies `img.src`.
- [ ] Native preflight, repository checks, cleanup, and immutable equality pass without forbidden action or product change.
