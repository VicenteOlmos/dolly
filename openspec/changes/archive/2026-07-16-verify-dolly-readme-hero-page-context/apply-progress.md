# Apply Progress: Verify Dolly README Hero in Page Context

**Mode**: Standard (`strict_tdd: false`)  
**Status**: implementation evidence complete; verification/archive tasks remain owned by later phases.

## Completed Tasks

- [x] 1.1 Native immutable preflight
- [x] 1.2 Native negative selector/index-state boundary coverage
- [x] 2.1 One page-context browser call
- [x] 2.2 Six width-first metrics panels
- [x] 2.3 One grid and cleanup
- [x] 3.1 Native postflight and equal manifest
- [x] 3.2 Evidence/report/progress artifacts

## Work Unit Evidence

| Evidence | Exact result |
|---|---|
| Focused test command | `go test ./...` — exit 0; `1185` assertions in `15` packages |
| Runtime harness | One `playwright_browser_run_code_unsafe` call — exit success; six ordered cases, one screenshot, no first failure |
| Rollback boundary | Remove only `openspec/changes/verify-dolly-readme-hero-page-context/evidence/hero-grid.png`, `verify-report.md`, and `apply-progress.md`; do not alter `README.md`, `assets/readme/hero.svg`, or authority inputs |

## Task 1.2: Negative Preflight Boundary Evidence

| Evidence | Exact result |
|---|---|
| Focused native command | Direct read-only Git/SHA-256 predicates — exit `0`; absolute root and current worktree root both `/home/vicho/programming/dolly`, exact staged projection `README.md`, `assets/readme/hero.svg`, non-empty index, and staged/worktree SHA-256 equality all `PASS`. |
| Runtime harness | N/A — selector rejection is native preflight policy; no executable selector input or browser/runtime boundary exists. |
| Rollback boundary | Revert this task checkbox and this section only; no repository index, targets, authority, browser evidence, grid, or prior artifacts changed. |

The policy accepts only the fixed absolute selector `/home/vicho/programming/dolly` when both the selector and resolved current worktree root equal that value. Therefore relative, child, and other-repository selectors fail the explicit absolute-selector/root-equality predicates before browser work. An empty index fails the non-empty-index predicate. Unstaged and `commit -a`-only states fail the exact two-path staged-projection and staged/worktree-target-equality predicates. Each rejection is terminal `FAIL`: no mutation, retry, browser call, script, alternate repository, child worktree, commit, or index update occurs. No executable selector input exists.

## Browser Result

- Browser call count: `1`; retries: `0`; screenshot count: `1`.
- Raw `sourceOuterHTML` was sole metrics source. `DOMParser` was inside `page.evaluate` only; data URI was used only as `img.src`.
- Every direct content-box target passed: `1012`, `1012`, `375`, `375`, `320`, `320` pixels. Minimum narrow text was `17.07` CSS pixels. Bounds, clipping, overlap, contrast, and alpha checks passed for all six panels.
- Grid SHA-256: `ac4243dd9b2ac0755257d5e52b0b8a36afd3cda13ea5abef103ba25904fb26c1`.

## Immutable Manifest

- Candidate tree: `dd1244c4ddad7fb0948e40d428ea7ae3fccc05f5` before and after.
- Target staged diff SHA-256: `3ba6ea5add538e61469d03ef2eecc668df64eabb1df53e0bbc7ed03acc43c60b` before and after.
- README / hero SHA-256: `b75719c7cff3732a5f13ed9e67bfde97c9108f44fd169e5803f1069da39b9c2b` / `d637aa03aad77961d7fbbf9139301c59be681b113f2fd3f754111cda55f4b7d7` before and after.

## Remaining Tasks

- [ ] 4.1 Run `sdd-verify`.
- [ ] 4.2 Run `sdd-archive` after verification.
