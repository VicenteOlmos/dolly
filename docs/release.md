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

The repo is `VicenteOlmos/dolly`. Use **release-first** ordering: build and verify the release while the repository stays private, then change visibility only after every gate passes.

1. Merge green changes to protected `main` on the remote. CI must pass on that exact commit SHA.
2. Apply the GitHub safeguard checklist (Unit D): repository metadata, pinned Actions policy, `release` environment, Dependabot alerts, secret scanning/push protection, and private vulnerability reporting. CodeQL analysis runs after the repo is public (task 4.5).
3. While the repository remains **private**, push an annotated tag `vX.Y.Z` (for example `v0.1.1`) from the green protected `main` tip. Do not overwrite an existing tag.
4. Let the `Release` workflow build and publish assets to GitHub Releases. Verify all seven release assets (six archives plus `checksums.txt`), archive contents (`dolly` / `dolly.exe`), and `dolly version` match the tag — still private.
5. Complete the maintainer pre-public checklist (branch/tag rulesets, first-time fork approval policy, and remaining Unit D gates from tasks 4.1–4.4).
6. Change repository visibility from **Private** to **Public** only after every prior gate is green.
7. Run anonymous post-public verification: clone, raw installer URLs, release downloads/checksums, public CI on a pull request, and security reporting links (task 4.5).

Optional local rebuild before tagging: `scripts/build-release-assets.sh dist` (override metadata with `VERSION`, `COMMIT`, `DATE` env vars).

The release workflow is tag-only on purpose. Tags and release assets are **immutable** once observers may rely on them. After the repository is public, never overwrite a published `vX.Y.Z` tag or its archives; publish a new patch tag (for example `v0.1.2`) from a fixed commit on protected `main`.

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

### Pre-public failure (repository still private)

No public consumer has observed the release yet. You may stop, remove the newly created private GitHub Release, delete the bad annotated tag (`git push --delete origin vX.Y.Z`), and revert preparatory settings or drafts. Fix the issue on a branch from protected `main`, merge through the normal PR process, and restart the release-first checklist from a clean gate.

### Post-public failure (repository public or assets observed)

Published tags and release assets are **immutable**. Do **not** delete, overwrite, or reuse a published `vX.Y.Z` tag as rollback — mirrors, caches, and installers may already hold the bad artifacts.

1. Contain impact: restrict visibility or Actions if needed; revoke or delist unsafe assets where GitHub allows.
2. Fix the issue on a branch from protected `main` and merge through the normal PR process.
3. Rotate any exposed credentials and record impact.
4. Tag a new patch version (for example `v0.1.2`) from the green protected `main` tip.
5. Push the annotated tag; the `Release` workflow publishes fresh immutable assets.
6. Verify installers pin or resolve the new release and that `checksums.txt` matches downloaded archives.

Clones, caches, and mirrors cannot be fully recalled; rotation, impact recording, and forward fixes are the supported recovery path. See [SECURITY.md](../SECURITY.md) for incident reporting.
