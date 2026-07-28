# Security policy

## Supported versions

Only the **latest** [GitHub Release](https://github.com/VicenteOlmos/dolly/releases) receives security fixes. Older tags remain available for download but are not maintained.

## Reporting vulnerabilities

Report security issues **privately** — do not open public issues, pull requests, or discussion threads for vulnerabilities.

1. Use [GitHub private vulnerability reporting](https://github.com/VicenteOlmos/dolly/security/advisories/new) for this repository.
2. Include enough detail for reproduction without posting live secrets, production DSNs, dump files, or working exploit payloads in public channels.
3. Allow reasonable time for triage and a fix on `main` before any public disclosure.

For general help boundaries, see [SUPPORT.md](SUPPORT.md).

## Release and incident response

- While the repository remains private, a failed preparatory release may be removed before changing visibility; once public, tags and download assets are **immutable**.
- Security fixes ship in a new SemVer patch tag, not by overwriting an existing release.
- Do **not** force-push `main` to recover from a bad release or disclosed secret — fix forward, rotate affected credentials, and revoke compromised artifacts.
- After publication, exposed secrets cannot be fully recalled from clones or caches; treat rotation and impact recording as mandatory.

## Database-operation safety

For operator checklists (DSNs, dumps, destructive restore, sanitization limits), see [docs/security.md](docs/security.md).
