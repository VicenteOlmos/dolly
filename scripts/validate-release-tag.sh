#!/bin/sh
# validate-release-tag.sh — stable-only release admission
set -eu

usage() {
	echo "usage: validate-release-tag.sh <tag> <tag-sha> <main-sha>" >&2
	exit 1
}

[ "$#" -eq 3 ] || usage

tag="$1"
tag_sha="$2"
main_sha="$3"

is_sha() {
	printf '%s' "$1" | grep -Eq '^[0-9a-fA-F]{40}$'
}

if ! printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "invalid tag: stable vX.Y.Z required (got: $tag)" >&2
	exit 1
fi

if ! is_sha "$tag_sha"; then
	echo "invalid tag-sha: expected 40-character commit SHA" >&2
	exit 1
fi

if ! is_sha "$main_sha"; then
	echo "invalid main-sha: expected 40-character commit SHA" >&2
	exit 1
fi

if [ "$tag_sha" != "$main_sha" ]; then
	echo "tagged commit does not match protected main tip" >&2
	exit 1
fi

version="${tag#v}"
printf 'tag=%s\n' "$tag"
printf 'version=%s\n' "$version"
