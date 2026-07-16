# Design: Verify Dolly README Hero in Page Context

## Technical Approach

Preserve the predecessor’s immutable acceptance contract and correct only its execution-context error. Native preflight binds the approved staged `README.md` and `assets/readme/hero.svg` to receipt `review-3c08bccd5245c532` and a deterministic manifest. One `playwright_browser_run_code_unsafe` call then loads raw SVG markup, creates six exact-width-first panels in page context, captures one grid, and cleans up. Native postflight and byte-equal manifests determine PASS; any failure records FAIL without retry, mutation, or delivery.

## Architecture Decisions

| Option | Tradeoff | Decision and rationale |
|---|---|---|
| Page-context parsing | Requires serializable inputs/results | Required: fixes predecessor `ReferenceError` while keeping metrics bound to raw source. |
| One call, six panels, one screenshot | No transient recovery | Required by authority; creates one atomic receipt. |
| Width-first evaluation | Later metrics may remain absent on failure | Prevents acceptance at wrapper-shrunk widths. |
| Native pre/post commands | More explicit evidence | Avoids generated harnesses and binds result to live repository state. |

## Data Flow

```text
native preflight → immutable manifest ─failure→ FAIL
  → ONE browser tool call
    → file SVG → sourceOuterHTML → about:blank
    → page-context parse → six decoded panels → width gate → metrics
    → one page.screenshot → cleanup
  → native checks → post-manifest equality → PASS/FAIL
```

## File Changes

| File | Action | Description |
|---|---|---|
| `openspec/changes/verify-dolly-readme-hero-page-context/design.md` | Create | This design. |
| `openspec/changes/verify-dolly-readme-hero-page-context/evidence/hero-grid.png` | Later create | Sole six-panel screenshot. |
| `openspec/changes/verify-dolly-readme-hero-page-context/verify-report.md` | Later create | Canonical PASS/FAIL evidence. |
| `README.md`, `assets/readme/hero.svg`, receipt/state, prior artifacts, refs | None | Immutable inputs. |

## Interfaces / Contracts

Inside the sole browser tool call, extract `sourceOuterHTML` from the file-loaded SVG without parsing, navigate to `about:blank`, then execute this shape:

```js
const result = await page.evaluate(
  async ({ sourceOuterHTML, cases }) => {
    const metricDoc = new DOMParser().parseFromString(sourceOuterHTML, 'image/svg+xml');
    // create imgs, decode, width-first, metrics
    const dataUri = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(sourceOuterHTML)}`;
    // For each case: create panel and img; assign dataUri only to img.src; append to document.
    await Promise.all(images.map((img) => img.decode()));
    // Require all six content-box widths within 0.01px before deriving remaining metrics from metricDoc.
    return serializableResult;
  },
  { sourceOuterHTML, cases }
);

await page.screenshot({ path: gridPath, fullPage: true });
```

Callback is `async` because `img.decode()` is awaited; `page.evaluate` awaits its returned promise. Data URI is transport only: assign it to `img.src`; never parse, inspect, or use the URI string for metrics. `page.screenshot` remains outside evaluation, after panels exist, but inside the same single browser tool call. Fixed order is `1012-light`, `1012-dark`, `375-light`, `375-dark`, `320-light`, `320-dark`. Images use direct content-box widths, no padding/border/max-width/shrinkage. Callback result contains six serializable case records and first failure; the outer call appends screenshot and cleanup metadata.

Preflight uses direct absolute-root Git/native commands to recompute root, exact two-path staged projection and 35-line scope, index/worktree equality, target and binary-diff hashes, candidate tree, approved receipt/state, prior inventory, refs, publication probes, and status. After successful browser cleanup, run `xmllint --noout`, `git diff --cached --check`, `go test ./...`, `go vet ./...`, and `go build -buildvcs=false ./...`; recompute the manifest excluding only declared new evidence/report. Mismatch or command failure is terminal.

## Testing Strategy

| Layer | What | Approach |
|---|---|---|
| Unit | N/A | No product logic or harness. |
| Integration | Authority and immutability | Native pre/post checks plus equal deterministic manifests. |
| E2E | Page-context acceptance | One call; six width-first cases, retained metrics, one screenshot, one cleanup. |

## Threat Matrix

| Boundary | Applicability | Safe/failure behavior | Planned RED tests |
|---|---|---|---|
| Documentation-like paths | N/A — fixed README/SVG paths are data, not executables | Never execute them | None |
| Git repository selection | Applicable | Exact absolute root proceeds; relative path, child `git -C`, or another repository stops before browser work | Reject relative, child, and different-repository selectors |
| Commit state | Applicable | Exact staged projection proceeds; empty index or `commit -a`-only/unstaged state stops | Accept staged; reject empty index and `commit -a`-only |
| Push state | N/A — push forbidden | No push path exists | None |
| PR commands | N/A — delivery/publication forbidden | No PR command exists | None |

## Migration / Rollout

No migration required. No retry, repair, commit, push, PR, or publication. Retain only complete declared evidence; preserve all immutable inputs and predecessor FAIL report.

## Open Questions

None.
