#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ci="$root/.github/workflows/ci.yml"
release="$root/.github/workflows/release.yml"
codeql="$root/.github/workflows/codeql.yml"
dependabot="$root/.github/dependabot.yml"
checkout=34e114876b0b11c390a56381ad16ebd13914f8d5
setup_go=40f1582b2485089dde7abd97c1529aa768e1baff
codeql_action=4187e74d05793876e9989daffde9c3e66b4acd07
upload_artifact=ea165f8d65b6e75b540449e92b4886f43607fa02
download_artifact=d3f86a106a0bac45b974a628896c90dbdf5c8093

for file in "$ci" "$release" "$codeql"; do
	grep -F "actions/checkout@$checkout # v4" "$file" >/dev/null
	grep -F "actions/setup-go@$setup_go # v5" "$file" >/dev/null
done

grep -F "github/codeql-action/init@$codeql_action # v3" "$codeql" >/dev/null
grep -F "github/codeql-action/analyze@$codeql_action # v3" "$codeql" >/dev/null
grep -F "actions/upload-artifact@$upload_artifact # v4" "$release" >/dev/null
grep -F "actions/download-artifact@$download_artifact # v4" "$release" >/dev/null

grep -F 'permissions:' "$ci" >/dev/null
grep -F 'contents: read' "$ci" >/dev/null
grep -F 'concurrency:' "$ci" >/dev/null
if grep -F 'pull_request_target' "$ci" >/dev/null; then
	echo "ci workflow must not use pull_request_target" >&2
	exit 1
fi

grep -F 'environment: release' "$release" >/dev/null
grep -F 'needs: build' "$release" >/dev/null
grep -F 'Release tag must be SemVer vX.Y.Z' "$release" >/dev/null
grep -F 'is not current protected main tip' "$release" >/dev/null
grep -F 'refusing to clobber' "$release" >/dev/null
grep -F 'sha256sum -c checksums.txt' "$release" >/dev/null
grep -F "grep -Fq \"dolly \$EXPECTED_VERSION \"" "$release" >/dev/null
if grep -F -- '--clobber' "$release" >/dev/null; then
	echo "release workflow must not clobber assets" >&2
	exit 1
fi
if grep -F "tag=\"\${{ steps.version.outputs.tag }}\"" "$release" >/dev/null; then
	echo "release tag expression is interpolated by shell" >&2
	exit 1
fi

grep -F 'package-ecosystem: gomod' "$dependabot" >/dev/null
grep -F 'package-ecosystem: github-actions' "$dependabot" >/dev/null
grep -F 'open-pull-requests-limit: 5' "$dependabot" >/dev/null

test -f "$root/.github/ISSUE_TEMPLATE/bug_report.yml"
test -f "$root/.github/ISSUE_TEMPLATE/feature_request.yml"
test -f "$root/.github/ISSUE_TEMPLATE/config.yml"
test -f "$root/.github/PULL_REQUEST_TEMPLATE.md"
test -f "$root/CODE_OF_CONDUCT.md"
test -f "$root/SUPPORT.md"
grep -F 'security/advisories/new' "$root/.github/ISSUE_TEMPLATE/config.yml" >/dev/null
grep -F 'security/advisories/new' "$root/SUPPORT.md" >/dev/null
