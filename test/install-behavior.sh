#!/bin/sh
# install.sh behavior tests — checksum policy (no network calls)
set -eu

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

pass() { printf "${GREEN}PASS${NC} %s\n" "$*"; }
fail_test() { printf "${RED}FAIL${NC} %s\n" "$*"; exit 1; }

script_dir="$(cd "$(dirname "$0")" && pwd)"
install_sh="$script_dir/../install.sh"

# Derive OS/arch the same way install.sh does so the fixture name matches.
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
	x86_64|amd64) arch="x86_64" ;;
	arm64|aarch64) arch="arm64" ;;
	*) echo "unsupported test architecture"; exit 1 ;;
esac
asset_name="dolly_${os}_${arch}.tar.gz"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# Create a fake dolly binary tar.gz
echo '#!/bin/sh' > "$tmpdir/dolly"
echo 'echo "dolly test binary"' >> "$tmpdir/dolly"
chmod +x "$tmpdir/dolly"
tar -czf "$tmpdir/$asset_name" -C "$tmpdir" dolly

# mock_no_checksums: asset present, NO checksums.txt
mkdir -p "$tmpdir/mock_no_checksums"
cp "$tmpdir/$asset_name" "$tmpdir/mock_no_checksums/"

# mock_with_checksums: asset present AND checksums.txt with entry
mkdir -p "$tmpdir/mock_with_checksums"
cp "$tmpdir/$asset_name" "$tmpdir/mock_with_checksums/"
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$tmpdir/mock_with_checksums" && sha256sum "$asset_name") \
		> "$tmpdir/mock_with_checksums/checksums.txt"
elif command -v shasum >/dev/null 2>&1; then
	(cd "$tmpdir/mock_with_checksums" && shasum -a 256 "$asset_name") \
		> "$tmpdir/mock_with_checksums/checksums.txt"
else
	# If neither sha256sum nor shasum is available, we can't verify
	# checksums but we can still test the fail-closed policy.
	echo "dummy  $asset_name" > "$tmpdir/mock_with_checksums/checksums.txt"
fi

# ── helpers ──────────────────────────────────────────────────────

# Run install.sh under the mock download dir, capture exit code.
# Accepts extra env vars as arguments.
run_install() {
	install_target="$tmpdir/install_dir"
	mkdir -p "$install_target"
	(
		DOLLY_REPO="test/test"
		DOLLY_INSTALL_DIR="$install_target"
		DOLLY_MOCK_DOWNLOAD_DIR="$1"
		export DOLLY_REPO DOLLY_INSTALL_DIR DOLLY_MOCK_DOWNLOAD_DIR
		shift
		for var in "$@"; do
			eval "export $var"
		done
		sh "$install_sh" >/dev/null 2>&1
	)
}

# ── tests ────────────────────────────────────────────────────────

echo ""

# Test 0: network downloader remains bounded when no mock transport is used.
grep -F -- '--connect-timeout 10 --max-time "$DOLLY_DOWNLOAD_TIMEOUT"' "$install_sh" >/dev/null || fail_test "stalled download: curl timeout missing"
grep -F -- '--timeout="$DOLLY_DOWNLOAD_TIMEOUT"' "$install_sh" >/dev/null || fail_test "stalled download: wget timeout missing"
pass "stalled download: curl and wget timeouts configured"

grep -F 'rm -f "$tmp_target" || true' "$install_sh" >/dev/null || fail_test "replacement cleanup: writable temp cleanup missing"
grep -F 'sudo rm -f "$sudo_tmp_target" || true' "$install_sh" >/dev/null || fail_test "replacement cleanup: sudo temp cleanup missing"
pass "replacement cleanup: temp files removed on failure"

# Test 1: latest fails when checksums are unavailable
if run_install "$tmpdir/mock_no_checksums"; then
	fail_test "latest: expected failure when checksums.txt is missing, but install succeeded"
fi
pass "latest: fails when checksums.txt is unavailable"

# Test 2: latest succeeds with DOLLY_ALLOW_UNVERIFIED=1
if run_install "$tmpdir/mock_no_checksums" "DOLLY_ALLOW_UNVERIFIED=1"; then
	pass "latest: succeeds with DOLLY_ALLOW_UNVERIFIED=1 (checksums skip)"
else
	fail_test "latest: expected success with DOLLY_ALLOW_UNVERIFIED=1, but install failed"
fi

# Test 3: corrupt checksum leaves an existing target untouched.
mkdir -p "$tmpdir/mock_corrupt"
cp "$tmpdir/$asset_name" "$tmpdir/mock_corrupt/"
printf '%064d  %s\n' 0 "$asset_name" > "$tmpdir/mock_corrupt/checksums.txt"
printf 'old binary\n' > "$tmpdir/install_dir/dolly"
if run_install "$tmpdir/mock_corrupt"; then
	fail_test "corrupt checksum: expected failure"
fi
if [ "$(cat "$tmpdir/install_dir/dolly")" != "old binary" ]; then
	fail_test "corrupt checksum replaced existing target"
fi
pass "corrupt checksum leaves existing target unchanged"

echo ""
printf '%s\n' "All install.sh behavior tests passed."
