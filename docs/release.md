# Local release readiness

The public module path is `github.com/VicenteOlmos/dolly`. The repository and Go module path are already chosen. This project is ready for local release-style builds; follow the public release steps below for distribution.

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

The repo is `VicenteOlmos/dolly`; all decisions below are finalized.

1. Push `main` to the public remote.
2. Tag the first release only after CI passes on the public remote.
3. Push an annotated tag `vX.Y.Z` (for example `v0.1.1`). The `Release` workflow runs vet, race tests, installer behavior tests, and Postgres integration before publishing assets to GitHub Releases.
4. Optional local rebuild: `scripts/build-release-assets.sh dist` (override metadata with `VERSION`, `COMMIT`, `DATE` env vars).

The release workflow is tag-only on purpose. If a release job needs a rebuild,
delete the broken release/tag and push a new patch tag instead of overwriting
assets under the same version.

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

If a release is broken after publishing:

### Rollback (undo the release)

1. Delete the GitHub Release and the associated tag (`git push --delete origin vX.Y.Z`).
2. If the release branch replaced the default branch content, force-push the previous good commit to `main`.
3. Remove the release archives and `checksums.txt` from the release page.
4. Verify `curl -fsSL https://raw.githubusercontent.com/VicenteOlmos/dolly/main/install.sh | DOLLY_REPO=VicenteOlmos/dolly sh` no longer installs the bad version.

### Fix-forward (patch)

1. Fix the issue on a branch off `main`.
2. Tag a new patch version (e.g. `v0.1.1`).
3. Push the tag; the `Release` workflow builds and uploads release assets.
4. The installer picks up the new latest automatically.
