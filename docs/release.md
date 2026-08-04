# Release policy and readiness

The public module path is `github.com/VicenteOlmos/dolly`. Releases use [SemVer](https://semver.org/) tags (`vX.Y.Z`), publish immutable assets to [GitHub Releases](https://github.com/VicenteOlmos/dolly/releases), and support **only the latest release** for security fixes. Report vulnerabilities privately via [GitHub private vulnerability reporting](https://github.com/VicenteOlmos/dolly/security/advisories/new) — not public issues.

| Requirement | Detail |
|---|---|
| Go toolchain | Match `go.mod` (currently Go 1.26.3+) |
| PostgreSQL | 16 for integration tests and recommended operator targets |
| Platforms | Linux, macOS, Windows (`x86_64` and `arm64`) |
| Release assets | Seven release assets (six archives plus `checksums.txt`); never overwrite an existing tag or asset |

Use this checklist for local release-style builds and maintainer publication gates.

## Preflight

Run these before a local release build or first commit:

```bash
go test ./...
go vet ./...
go build -buildvcs=false ./cmd/dolly
make preflight
```

Optional integration check, when the dev database is running:

```bash
docker compose up -d
export DOLLY_TEST_PG_DSN='postgres://dolly:dolly@127.0.0.1:5433/dolly?sslmode=disable'
make test-integration
```

## Local versioned build

Build a local binary with version metadata:

```bash
make build-versioned VERSION=0.0.0-local
./bin/dolly version
```

Equivalent raw command:

```bash
go build -buildvcs=false \
  -ldflags "-X main.version=0.0.0-local -X main.commit=$(git rev-parse --short HEAD 2>/dev/null || echo local) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o ./bin/dolly ./cmd/dolly
```

## First local commit checklist

Before the first local commit:

- [ ] Confirm `go test ./...`, `go vet ./...`, and `go build -buildvcs=false ./cmd/dolly` pass.
- [ ] Review `git status --short` for accidental secrets, dumps, databases, binaries, or generated files.
- [ ] Confirm `LICENSE` is present and matches the intended license.
- [ ] Confirm `go.mod` module path matches `github.com/VicenteOlmos/dolly`.
- [ ] Use a Conventional Commit message, for example `chore: prepare local release docs`.
- [ ] Do not add AI attribution or `Co-Authored-By` trailers.

## Public release steps

The repository is public (`VicenteOlmos/dolly`) with protected `main`. Publication is fail-closed: every gate must pass on the exact commit SHA before tagging, and operator documentation that references a new version advances only after independent public proof.

1. Merge green changes to protected `main`. CI and CodeQL must pass on that exact commit SHA.
2. Freeze the candidate SHA: confirm local `main` equals `origin/main` at the candidate commit, the worktree is clean, no open release delivery is in progress, and the target tag does not already exist locally or on GitHub.
3. Run pre-tag verification on the frozen SHA: `make preflight`, full race suite, PostgreSQL 16 serial integration tests, installer behavior suites, and both release policy tests (`sh test/release-workflow.sh`, `sh test/release-tag-behavior.sh`). Record evidence.
4. Push an annotated stable tag `vX.Y.Z` (for example `v0.3.4`) from the verified protected `main` tip only. CI runs `test/release-tag-behavior.sh`, which exercises the shared `scripts/validate-release-tag.sh` contract; only the Release workflow invokes that validator with live SHAs for stable/exact-main admission at publication. Do not overwrite an existing tag.
5. Let the `Release` workflow build, attest, and publish assets to GitHub Releases. Verify the run completes, `gh release verify` passes, and the release is latest stable, non-draft, non-prerelease, immutable, and attested.
6. Collect independent public proof: exactly seven nonempty release assets (six archives plus `checksums.txt`), checksum matrix SHA-256 match, safe single-executable archives, six `go version -m` results at the release version and candidate commit, Unix/PowerShell pinned and latest installs, and updater state (`previous→available`, `current→current`).
7. Finalize operator documentation only after proof: move changelog bullets from `Unreleased` to the canonical linked/dated release section and advance README install pins/examples. Rerun exact-main CI on the docs PR.

Optional local rebuild before tagging: `scripts/build-release-assets.sh dist` (override metadata with `VERSION`, `COMMIT`, `DATE` env vars).

The release workflow is tag-only on purpose. Tags and release assets are **immutable** once observers may rely on them. Never overwrite a published `vX.Y.Z` tag or its archives; publish a new patch tag (for example `v0.3.5`) from a fixed commit on protected `main`.

### Release assets

`scripts/build-release-assets.sh` writes these files under `dist/`:

- `dolly_linux_x86_64.tar.gz`, `dolly_linux_arm64.tar.gz`
- `dolly_darwin_x86_64.tar.gz`, `dolly_darwin_arm64.tar.gz`
- `dolly_windows_x86_64.zip`, `dolly_windows_arm64.zip`
- `checksums.txt` — SHA-256 lines for every archive above

Each `.tar.gz` contains a `dolly` binary; each `.zip` contains `dolly.exe`.

## Curl installer release checklist

Before publishing `install.sh` and `install.ps1` as a public install path:

- [ ] Confirm the GitHub repo is public (VicenteOlmos/dolly).
- [ ] Publish release archives named `dolly_linux_x86_64.tar.gz`, `dolly_linux_arm64.tar.gz`, `dolly_darwin_x86_64.tar.gz`, `dolly_darwin_arm64.tar.gz`, `dolly_windows_x86_64.zip`, and `dolly_windows_arm64.zip`.
- [ ] Ensure each `.tar.gz` archive contains a `dolly` binary; each `.zip` archive contains `dolly.exe`.
- [ ] Publish `checksums.txt` with SHA-256 entries for every archive.
- [ ] Test the latest-release command on Linux/macOS:

  ```bash
  curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_REPO=VicenteOlmos/dolly sh
  ```

- [ ] Test the latest-release command on Windows:

  ```powershell
  irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
  ```

- [ ] Test a pinned release command on Linux and macOS:

  ```bash
curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_REPO=VicenteOlmos/dolly DOLLY_VERSION=0.1.1 sh
  ```

- [ ] Test a pinned release command on Windows:

  ```powershell
$env:DOLLY_VERSION="0.1.1"; irm https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.ps1 | iex
  ```

- [ ] Run `dolly version` after install and confirm the printed version matches the release.

## Bad release recovery

**Never force-push `main`** to undo a release or disclosed secret.

### Pre-tag failure

Stop before pushing the annotated tag to the remote. You may delete an unpushed local tag only (`git tag -d vX.Y.Z`). Fix the issue on a branch from protected `main`, merge through the normal PR process, and restart the public release checklist from a clean gate.

Any failure after the tag is pushed to the public remote is post-publication. Do not delete, overwrite, or reuse the remote tag or release; publish a new patch version (for example `v0.3.5`) per the steps below.

### Post-public failure (tag pushed to public remote)

Published tags and release assets are **immutable**. Do **not** delete, overwrite, or reuse a published `vX.Y.Z` tag as rollback — mirrors, caches, and installers may already hold the bad artifacts.

1. Contain impact: disable or restrict affected workflows or assets where GitHub allows.
2. Fix the issue on a branch from protected `main` and merge through the normal PR process.
3. Rotate any exposed credentials and record impact.
4. Tag a new patch version (for example `v0.3.5`) from the green protected `main` tip.
5. Push the annotated tag; the `Release` workflow publishes fresh immutable assets.
6. Verify installers pin or resolve the new release and that `checksums.txt` matches downloaded archives.

Clones, caches, and mirrors cannot be fully recalled; rotation, impact recording, and forward fixes are the supported recovery path. See [SECURITY.md](../SECURITY.md) for incident reporting.
