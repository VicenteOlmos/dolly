#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
release="$root/.github/workflows/release.yml"
dependabot="$root/.github/dependabot.yml"

checkout=3d3c42e5aac5ba805825da76410c181273ba90b1
setup_go=b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
upload_artifact=043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
download_artifact=d3f86a106a0bac45b974a628896c90dbdf5c8093

grep -F "actions/checkout@$checkout # v7.0.1" "$release" >/dev/null
grep -F "actions/setup-go@$setup_go # v7.0.0" "$release" >/dev/null
grep -F "actions/upload-artifact@$upload_artifact # v7.0.1" "$release" >/dev/null
grep -F "actions/download-artifact@$download_artifact # v4" "$release" >/dev/null

grep -F 'environment: release' "$release" >/dev/null
grep -F 'needs: build' "$release" >/dev/null

publish_block=$(awk '/^  publish:/{found=1} found{print}' "$release")
printf '%s\n' "$publish_block" | grep -F "GH_REPO: \${{ github.repository }}" >/dev/null
if printf '%s\n' "$publish_block" | grep -F 'actions/checkout@' >/dev/null; then
	echo "publish job must not checkout source; use GH_REPO for gh release context" >&2
	exit 1
fi
printf '%s\n' "$publish_block" | grep -F 'gh release create' >/dev/null
printf '%s\n' "$publish_block" | grep -F 'gh release upload' >/dev/null
printf '%s\n' "$publish_block" | grep -F 'gh release edit' >/dev/null
printf '%s\n' "$publish_block" | grep -F -- '--verify-tag' >/dev/null
if printf '%s\n' "$publish_block" | grep -F -- '--repo ' >/dev/null; then
	:
elif ! printf '%s\n' "$publish_block" | grep -F 'GH_REPO:' >/dev/null; then
	echo "publish job gh release commands need GH_REPO or explicit --repo" >&2
	exit 1
fi
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
