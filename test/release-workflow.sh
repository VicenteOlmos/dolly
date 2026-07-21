#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ci="$root/.github/workflows/ci.yml"
release="$root/.github/workflows/release.yml"
checkout=34e114876b0b11c390a56381ad16ebd13914f8d5
setup_go=40f1582b2485089dde7abd97c1529aa768e1baff

for file in "$ci" "$release"; do
	grep -F "actions/checkout@$checkout # v4" "$file" >/dev/null
	grep -F "actions/setup-go@$setup_go # v5" "$file" >/dev/null
done

grep -F 'RELEASE_TAG: ${{ steps.version.outputs.tag }}' "$release" >/dev/null
grep -F 'gh release create "$RELEASE_TAG" --draft --verify-tag --title "$RELEASE_TAG" --generate-notes' "$release" >/dev/null
grep -F 'gh release upload "$RELEASE_TAG" dist/* --clobber' "$release" >/dev/null
grep -F "gh release view \"\$RELEASE_TAG\" --json assets --jq '.assets | length'" "$release" >/dev/null
grep -F 'gh release edit "$RELEASE_TAG" --draft=false' "$release" >/dev/null
if grep -F 'tag="${{ steps.version.outputs.tag }}"' "$release" >/dev/null; then
	echo "release tag expression is interpolated by shell" >&2
	exit 1
fi
