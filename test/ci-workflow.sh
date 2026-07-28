#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ci="$root/.github/workflows/ci.yml"
codeql="$root/.github/workflows/codeql.yml"
release="$root/.github/workflows/release.yml"

checkout=3d3c42e5aac5ba805825da76410c181273ba90b1
setup_go=b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
upload_artifact=043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
codeql_action=e4fba868fa4b1b91e1fdab776edc8cfbe6e9fb81

old_checkout=34e114876b0b11c390a56381ad16ebd13914f8d5
old_setup_go=40f1582b2485089dde7abd97c1529aa768e1baff
old_upload_artifact=ea165f8d65b6e75b540449e92b4886f43607fa02
old_codeql_action=4187e74d05793876e9989daffde9c3e66b4acd07

count_matches() {
	needle=$1
	shift
	total=0
	for file in "$@"; do
		n=$(grep -cF "$needle" "$file" || true)
		total=$((total + n))
	done
	printf '%s' "$total"
}

for file in "$ci" "$codeql"; do
	grep -F "actions/checkout@$checkout # v7.0.1" "$file" >/dev/null
	grep -F "actions/setup-go@$setup_go # v7.0.0" "$file" >/dev/null
done

grep -F "github/codeql-action/init@$codeql_action # v4.37.3" "$codeql" >/dev/null
grep -F "github/codeql-action/analyze@$codeql_action # v4.37.3" "$codeql" >/dev/null
grep -F 'if: github.event.repository.private == false' "$codeql" >/dev/null
grep -F 'workflow_dispatch:' "$codeql" >/dev/null
grep -F 'security-events: write' "$codeql" >/dev/null
grep -F 'pull_request:' "$codeql" >/dev/null
grep -F 'schedule:' "$codeql" >/dev/null
grep -F 'branches: [main]' "$codeql" >/dev/null

grep -F 'permissions:' "$ci" >/dev/null
grep -F 'contents: read' "$ci" >/dev/null
grep -F 'concurrency:' "$ci" >/dev/null
if grep -F 'pull_request_target' "$ci" >/dev/null; then
	echo "ci workflow must not use pull_request_target" >&2
	exit 1
fi

checkout_count=$(count_matches "actions/checkout@$checkout" "$ci" "$codeql" "$release")
setup_go_count=$(count_matches "actions/setup-go@$setup_go" "$ci" "$codeql" "$release")
upload_count=$(count_matches "actions/upload-artifact@$upload_artifact" "$ci" "$codeql" "$release")

if [ "$checkout_count" -ne 4 ]; then
	echo "expected checkout@$checkout in 4 workflow uses, got $checkout_count" >&2
	exit 1
fi
if [ "$setup_go_count" -ne 4 ]; then
	echo "expected setup-go@$setup_go in 4 workflow uses, got $setup_go_count" >&2
	exit 1
fi
if [ "$upload_count" -ne 1 ]; then
	echo "expected upload-artifact@$upload_artifact in 1 workflow use, got $upload_count" >&2
	exit 1
fi

codeql_init_count=$(count_matches "github/codeql-action/init@$codeql_action" "$codeql")
codeql_analyze_count=$(count_matches "github/codeql-action/analyze@$codeql_action" "$codeql")
codeql_count=$((codeql_init_count + codeql_analyze_count))
if [ "$codeql_count" -ne 2 ]; then
	echo "expected 2 codeql-action uses at $codeql_action, got $codeql_count" >&2
	exit 1
fi

total=$((checkout_count + setup_go_count + upload_count + codeql_count))
if [ "$total" -ne 11 ]; then
	echo "expected 11 targeted workflow uses, got $total" >&2
	exit 1
fi

for file in "$ci" "$codeql" "$release"; do
	if grep -F "$old_checkout" "$file" >/dev/null; then
		echo "old checkout pin still present in $file" >&2
		exit 1
	fi
	if grep -F "$old_setup_go" "$file" >/dev/null; then
		echo "old setup-go pin still present in $file" >&2
		exit 1
	fi
	if grep -F "$old_upload_artifact" "$file" >/dev/null; then
		echo "old upload-artifact pin still present in $file" >&2
		exit 1
	fi
	if grep -F "$old_codeql_action" "$file" >/dev/null; then
		echo "old codeql-action pin still present in $file" >&2
		exit 1
	fi
	if grep -F 'github/codeql-action/' "$file" | grep -F '# v3' >/dev/null 2>&1; then
		echo "old codeql-action comment still present in $file" >&2
		exit 1
	fi
	if grep -F '# v4' "$file" | grep -F 'actions/checkout@' >/dev/null 2>&1; then
		echo "old checkout comment still present in $file" >&2
		exit 1
	fi
	if grep -F '# v5' "$file" | grep -F 'actions/setup-go@' >/dev/null 2>&1; then
		echo "old setup-go comment still present in $file" >&2
		exit 1
	fi
done

if grep -F '# v4' "$release" | grep -F 'actions/upload-artifact@' >/dev/null 2>&1; then
	echo "old upload-artifact comment still present in release workflow" >&2
	exit 1
fi
