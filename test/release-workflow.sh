#!/bin/sh
# Single-quoted grep patterns intentionally match release.yml source literals, not runtime expansion.
# shellcheck disable=SC2016
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
release="$root/.github/workflows/release.yml"
dependabot="$root/.github/dependabot.yml"
validator="$root/scripts/validate-release-tag.sh"

checkout=3d3c42e5aac5ba805825da76410c181273ba90b1
setup_go=b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
upload_artifact=043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
download_artifact=d3f86a106a0bac45b974a628896c90dbdf5c8093

extract_meta_block() {
	awk '
		/^      - name: Validate SemVer tag/ { found=1 }
		found && /^      - name:/ && !/Validate SemVer tag/ { exit }
		found { print }
	' "$1"
}

assert_meta_wiring() {
	file=$1
	block=$(extract_meta_block "$file")
	if [ -z "$block" ]; then
		echo "meta step block missing in $file" >&2
		return 1
	fi
	if ! printf '%s\n' "$block" | grep -F 'tag="${GITHUB_REF#refs/tags/}"' >/dev/null; then
		return 1
	fi
	if ! printf '%s\n' "$block" | grep -F 'gh api "repos/$GITHUB_REPOSITORY/git/ref/heads/main"' >/dev/null; then
		return 1
	fi
	if ! printf '%s\n' "$block" | grep -F '.object.sha' >/dev/null; then
		return 1
	fi
	if ! printf '%s\n' "$block" | grep -F 'sh scripts/validate-release-tag.sh "$tag" "$GITHUB_SHA" "$main_sha"' >/dev/null; then
		return 1
	fi
	if ! printf '%s\n' "$block" | grep -F 'printf '"'"'%s\n'"'"' "$output" >> "$GITHUB_OUTPUT"' >/dev/null; then
		return 1
	fi
	return 0
}

assert_validator_diagnostics() {
	grep -F 'stable vX.Y.Z required' "$validator" >/dev/null
	grep -F 'tagged commit does not match protected main tip' "$validator" >/dev/null
}

assert_oracle_fails_without() {
	label=$1
	mutation=$2
	tmp=$(mktemp)
	case "$mutation" in
	no_validator)
		sed '/validate-release-tag\.sh/d' "$release" > "$tmp"
		;;
	no_api_main_sha)
		sed '/gh api "repos\/\$GITHUB_REPOSITORY\/git\/ref\/heads\/main"/d' "$release" > "$tmp"
		;;
	no_github_output)
		sed '/>> "$GITHUB_OUTPUT"/d' "$release" > "$tmp"
		;;
	*)
		echo "unknown mutation: $mutation" >&2
		rm -f "$tmp"
		exit 1
		;;
	esac
	if assert_meta_wiring "$tmp"; then
		echo "oracle must fail when $label is missing" >&2
		rm -f "$tmp"
		exit 1
	fi
	rm -f "$tmp"
}

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

assert_meta_wiring "$release"
assert_validator_diagnostics

grep -F 'refusing to clobber' "$release" >/dev/null
grep -F 'sha256sum -c checksums.txt' "$release" >/dev/null
grep -F "grep -Fq \"dolly \$EXPECTED_VERSION \"" "$release" >/dev/null
grep -F 'test "$(find dist -maxdepth 1 -type f | wc -l)" -eq 7' "$release" >/dev/null
grep -F 'dolly_linux_x86_64.tar.gz' "$release" >/dev/null
grep -F 'dolly_linux_arm64.tar.gz' "$release" >/dev/null
grep -F 'dolly_darwin_x86_64.tar.gz' "$release" >/dev/null
grep -F 'dolly_darwin_arm64.tar.gz' "$release" >/dev/null
grep -F 'dolly_windows_x86_64.zip' "$release" >/dev/null
grep -F 'dolly_windows_arm64.zip' "$release" >/dev/null
grep -F 'checksums.txt' "$release" >/dev/null
if grep -F -- '--clobber' "$release" >/dev/null; then
	echo "release workflow must not clobber assets" >&2
	exit 1
fi
if grep -F "tag=\"\${{ steps.version.outputs.tag }}\"" "$release" >/dev/null; then
	echo "release tag expression is interpolated by shell" >&2
	exit 1
fi

assert_oracle_fails_without "validator call" no_validator
assert_oracle_fails_without "API main SHA fetch" no_api_main_sha
assert_oracle_fails_without "GITHUB_OUTPUT wiring" no_github_output

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
