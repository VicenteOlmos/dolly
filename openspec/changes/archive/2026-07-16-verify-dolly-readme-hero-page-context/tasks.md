# Tasks: Verify Dolly README Hero in Page Context

## Review Workload Forecast

| Field | Value |
|---|---|
| Estimated changed lines | 120–260 authored lines; evidence excluded |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | single-pr |
| Chain strategy | size-exception |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: size-exception
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|---|---|---|---|---|---|
| 1 | Produce immutable acceptance evidence | PR 1 | Native pre/post commands; `go test ./...` | One browser call + one screenshot | Remove new evidence/report only |

## Phase 1: Native Preflight

- [x] 1.1 Run direct absolute-root native preflight: bind staged `README.md`/`assets/readme/hero.svg` to receipt, verify exact projection, hashes, scope, status, refs, prior artifacts, and publication state; stop before browser on mismatch.
- [x] 1.2 RED: reject relative/child/other-repository selectors and empty, unstaged, or `commit -a`-only states; record terminal FAIL without mutation or retry.

## Phase 2: Single Browser Acceptance

- [x] 2.1 Execute exactly one `playwright_browser_run_code_unsafe` call; pass raw `sourceOuterHTML` and `cases` to the exact `page.evaluate(async ({ sourceOuterHTML, cases }) => { ... })` shape from `design.md`, with `DOMParser` only inside callback.
- [x] 2.2 In that call, evaluate width-first panels in fixed order `1012-light`, `1012-dark`, `375-light`, `375-dark`, `320-light`, `320-dark`; retain geometry, readability, clipping, overlap, contrast, alpha metrics and direct content-box width gates.
- [x] 2.3 Capture exactly one six-panel screenshot grid, use encoded markup only for `img.src`, then perform one cleanup; any browser/metric failure records FAIL without retry, extra call, target/state edit, or delivery.

## Phase 3: Native Postflight and Evidence

- [x] 3.1 After cleanup run `xmllint --noout`, `git diff --cached --check`, `go test ./...`, `go vet ./...`, and `go build -buildvcs=false ./...`; recompute post-manifest excluding only declared evidence/report and require byte equality.
- [x] 3.2 Write `evidence/hero-grid.png`, canonical `verify-report.md`, and `apply-progress.md`; include exact call count, six panels, metrics, manifests, commands, PASS/FAIL, and forbidden-action status.

## Phase 4: SDD Completion

- [x] 4.1 Run conventional `sdd-verify` against proposal/spec/design/tasks and repository evidence; preserve FAIL if any gate failed.
- [x] 4.2 Run `sdd-archive` only after verification; archive delta/report without commit, push, PR, or publication.
