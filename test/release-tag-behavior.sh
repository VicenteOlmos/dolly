#!/bin/sh
# validate-release-tag.sh behavior tests — stable-only release admission
set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { printf "${GREEN}PASS${NC} %s\n" "$*"; }
fail_test() { printf "${RED}FAIL${NC} %s\n" "$*"; exit 1; }

script_dir="$(cd "$(dirname "$0")" && pwd)"
validator="$script_dir/../scripts/validate-release-tag.sh"

SHA_MAIN="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
SHA_OTHER="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

run_validator() {
	_outfile="$(mktemp)"
	_errfile="$(mktemp)"
	set +e
	sh "$validator" "$@" >"$_outfile" 2>"$_errfile"
	_status=$?
	set -e
	_stdout="$(cat "$_outfile")"
	_stderr="$(cat "$_errfile")"
	rm -f "$_outfile" "$_errfile"
}

assert_success() {
	_name="$1"
	_tag="$2"
	_tag_sha="$3"
	_main_sha="$4"
	_expected_tag="$5"
	_expected_version="$6"

	run_validator "$_tag" "$_tag_sha" "$_main_sha"
	if [ "$_status" -ne 0 ]; then
		printf 'stderr: %s\n' "$_stderr" >&2
		fail_test "$_name: expected success, got exit $_status"
	fi
	if [ "$_stdout" != "tag=$_expected_tag
version=$_expected_version" ]; then
		printf 'stdout: %s\n' "$_stdout" >&2
		fail_test "$_name: unexpected stdout"
	fi
	pass "$_name"
}

assert_failure() {
	_name="$1"
	_tag="$2"
	_tag_sha="$3"
	_main_sha="$4"

	run_validator "$_tag" "$_tag_sha" "$_main_sha"
	if [ "$_status" -eq 0 ]; then
		printf 'stdout: %s\n' "$_stdout" >&2
		fail_test "$_name: expected failure, got success"
	fi
	if [ -z "$_stderr" ]; then
		fail_test "$_name: expected stderr diagnostic"
	fi
	pass "$_name"
}

assert_usage_failure() {
	_name="$1"
	shift

	run_validator "$@"
	if [ "$_status" -eq 0 ]; then
		fail_test "$_name: expected usage failure, got success"
	fi
	pass "$_name"
}

[ -f "$validator" ] || fail_test "validator script missing: $validator"

echo ""

assert_success \
	"stable main-tip tag accepted" \
	"v1.2.3" "$SHA_MAIN" "$SHA_MAIN" \
	"v1.2.3" "1.2.3"

assert_failure \
	"prerelease tag rejected" \
	"v1.2.3-rc.1" "$SHA_MAIN" "$SHA_MAIN"

assert_failure \
	"build metadata tag rejected" \
	"v1.2.3+build" "$SHA_MAIN" "$SHA_MAIN"

assert_failure \
	"malformed tag rejected (missing patch)" \
	"v1.2" "$SHA_MAIN" "$SHA_MAIN"

assert_failure \
	"malformed tag rejected (missing v prefix)" \
	"1.2.3" "$SHA_MAIN" "$SHA_MAIN"

assert_failure \
	"non-v tag rejected" \
	"release-1.2.3" "$SHA_MAIN" "$SHA_MAIN"

assert_failure \
	"non-main SHA rejected" \
	"v1.2.3" "$SHA_OTHER" "$SHA_MAIN"

assert_usage_failure \
	"wrong arg count rejected (too few)" \
	"v1.2.3" "$SHA_MAIN"

assert_usage_failure \
	"wrong arg count rejected (too many)" \
	"v1.2.3" "$SHA_MAIN" "$SHA_MAIN" "extra"

echo ""
printf '%s\n' "All release-tag behavior tests passed."
