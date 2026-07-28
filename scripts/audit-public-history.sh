#!/bin/sh
# Deterministic retained-ref and history secret scan for public release gates.
#
# Usage:
#   scripts/audit-public-history.sh local  [evidence_dir]
#   scripts/audit-public-history.sh remote [evidence_dir]
#   scripts/audit-public-history.sh both   [evidence_dir]
#
# Evidence is written under EVIDENCE_DIR (default: /tmp/opencode/dolly-public-audit-<ts>).
# Real credential findings block publication and must be rotated before rewrite.
# Operational scanner, git, or transport failures fail closed (nonzero exit).

set -eu

MODE="${1:-}"
EVIDENCE_DIR="${2:-${EVIDENCE_DIR:-}}"
REPO_ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
REMOTE_NAME="${REMOTE_NAME:-origin}"
GITLEAKS_CONFIG="${GITLEAKS_CONFIG:-$REPO_ROOT/.gitleaks.toml}"
TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
umask 077

usage() {
	cat <<'EOF'
Usage: scripts/audit-public-history.sh <local|remote|both> [evidence_dir]

Modes:
  local   Inventory local refs/objects, materialize unreachable payloads, scan history
  remote  Fresh bare mirror (heads, tags, refs/pull/*/{head,merge}), scan --all
  both    Run local + remote and compare ref manifests for drift

Environment:
  EVIDENCE_DIR     Output directory (default: /tmp/opencode/dolly-public-audit-<timestamp>)
  GITLEAKS_CONFIG  Gitleaks config path (default: repo .gitleaks.toml)
  REMOTE_URL       Override remote URL (default: remote named by REMOTE_NAME)
  REMOTE_NAME      Git remote name (default: origin)
EOF
}

die() {
	echo "error: $*" >&2
	exit 1
}

require_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

# Run git with C locale so machine-parsed output (fsck unreachable lines) stays English.
git_c() {
	LC_ALL=C LANG=C git "$@"
}

validate_gitleaks_config() {
	require_cmd gitleaks
	[ -f "$GITLEAKS_CONFIG" ] || die "gitleaks config not found: $GITLEAKS_CONFIG"
	# Prove config parses with installed Gitleaks (fail closed on invalid TOML/regex).
	set +e
	gitleaks detect \
		--config "$GITLEAKS_CONFIG" \
		--source "$REPO_ROOT" \
		--log-opts="HEAD~0..HEAD" \
		--report-format json \
		--report-path "$EVIDENCE_DIR/.gitleaks-config-probe.json" \
		--redact=100 >/dev/null 2>"$EVIDENCE_DIR/.gitleaks-config-probe.stderr"
	probe_status=$?
	set -e
	if [ ! -f "$EVIDENCE_DIR/.gitleaks-config-probe.json" ]; then
		if [ -s "$EVIDENCE_DIR/.gitleaks-config-probe.stderr" ]; then
			cat "$EVIDENCE_DIR/.gitleaks-config-probe.stderr" >&2
		fi
		die "gitleaks config validation failed (status=$probe_status)"
	fi
	python3 - "$EVIDENCE_DIR/.gitleaks-config-probe.json" <<'PY' || die "gitleaks config probe produced invalid JSON"
import json, sys
json.load(open(sys.argv[1]))
PY
	rm -f "$EVIDENCE_DIR/.gitleaks-config-probe.json" "$EVIDENCE_DIR/.gitleaks-config-probe.stderr"
}

prepare_evidence_dir() {
	if [ -z "$EVIDENCE_DIR" ]; then
		EVIDENCE_DIR="/tmp/opencode/dolly-public-audit-$TIMESTAMP"
	fi
	if [ -e "$EVIDENCE_DIR" ]; then
		if [ -n "$(ls -A "$EVIDENCE_DIR" 2>/dev/null || true)" ]; then
			echo "reinitializing stale evidence directory: $EVIDENCE_DIR" >&2
			rm -rf "$EVIDENCE_DIR"
		fi
	fi
	mkdir -p "$EVIDENCE_DIR"
	chmod 700 "$EVIDENCE_DIR"
}

write_header() {
	{
		echo "audit_mode=$MODE"
		echo "repo_root=$REPO_ROOT"
		echo "evidence_dir=$EVIDENCE_DIR"
		echo "timestamp=$TIMESTAMP"
		echo "gitleaks_config=$GITLEAKS_CONFIG"
	} >"$EVIDENCE_DIR/audit-manifest.txt"
}

inventory_refs() {
	target="$1"
	out="$2"
	(
		cd "$target"
		git for-each-ref --format='%(refname) %(objectname)' refs/heads refs/tags refs/pull
		git show-ref --head --dereference HEAD | awk '{print "refs/heads/HEAD", $1}'
	) | awk '
		{
			ref = $1
			sha = $2
			gsub(/^refs\/remotes\/origin\//, "refs/heads/", ref)
			print ref, sha
		}
	' | sort -u >"$out"
}

remote_ls_refs() {
	url="$1"
	out="$2"
	heads_tags="$out.tmp-heads-tags"
	pull_refs="$out.tmp-pull-refs"

	git ls-remote --heads --tags "$url" >"$heads_tags" \
		|| die "git ls-remote --heads --tags failed for $url"
	set +e
	git ls-remote "$url" 'refs/pull/*/head' 'refs/pull/*/merge' >"$pull_refs" 2>"$out.tmp-pull-stderr"
	pull_status=$?
	set -e
	if [ "$pull_status" -ne 0 ]; then
		if [ -s "$out.tmp-pull-stderr" ]; then
			cat "$out.tmp-pull-stderr" >&2
		fi
		die "git ls-remote pull refs failed for $url (status=$pull_status)"
	fi
	cat "$heads_tags" "$pull_refs" | awk '{print $2, $1}' | sort -u >"$out"
	rm -f "$heads_tags" "$pull_refs" "$out.tmp-pull-stderr"
}

materialize_unreachable() {
	repo="$1"
	out_dir="$2"
	disposition="$3"
	mkdir -p "$out_dir"

	fsck_out="$out_dir/fsck-unreachable.txt"
	set +e
	git_c -C "$repo" fsck --unreachable --no-reflogs >"$fsck_out" 2>&1
	fsck_status=$?
	set -e
	if [ "$fsck_status" -ne 0 ]; then
		cat "$fsck_out" >&2
		die "git fsck --unreachable failed in $repo (status=$fsck_status)"
	fi

	awk '/^unreachable commit / {print $3}' "$fsck_out" | sort -u >"$out_dir/unreachable-commits.txt"
	awk '/^unreachable blob / {print $3}' "$fsck_out" | sort -u >"$out_dir/unreachable-blobs.txt"
	awk '/^unreachable tree / {print $3}' "$fsck_out" | sort -u >"$out_dir/unreachable-trees.txt"
	awk '/^unreachable tag / {print $3}' "$fsck_out" | sort -u >"$out_dir/unreachable-tags.txt"

	: >"$disposition"
	while read -r sha; do
		[ -n "$sha" ] || continue
		dest="$out_dir/commits/$sha"
		mkdir -p "$dest/tree"
		git -C "$repo" cat-file -p "$sha" >"$dest/commit-object.txt"
		git -C "$repo" log -1 --format=fuller "$sha" >"$dest/commit-message.txt"
		git -C "$repo" archive "$sha" 2>/dev/null | tar -x -C "$dest/tree" 2>/dev/null || true
		echo "$sha commit object+message+tree materialized" >>"$disposition"
	done <"$out_dir/unreachable-commits.txt"

	while read -r sha; do
		[ -n "$sha" ] || continue
		dest="$out_dir/blobs/$sha"
		mkdir -p "$dest"
		git -C "$repo" cat-file -p "$sha" >"$dest/blob.txt"
		echo "$sha blob materialized" >>"$disposition"
	done <"$out_dir/unreachable-blobs.txt"

	while read -r sha; do
		[ -n "$sha" ] || continue
		dest="$out_dir/trees/$sha"
		mkdir -p "$dest"
		git -C "$repo" cat-file -p "$sha" >"$dest/tree.txt"
		git -C "$repo" ls-tree -r "$sha" >"$dest/entries.txt" 2>/dev/null || true
		echo "$sha tree materialized" >>"$disposition"
	done <"$out_dir/unreachable-trees.txt"

	while read -r sha; do
		[ -n "$sha" ] || continue
		dest="$out_dir/tags/$sha"
		mkdir -p "$dest"
		git -C "$repo" cat-file -p "$sha" >"$dest/tag-object.txt"
		echo "$sha tag object materialized" >>"$disposition"
	done <"$out_dir/unreachable-tags.txt"

	# Design-required unreachable commit (may not appear in fsck output on all clones).
	if git -C "$repo" cat-file -e '3f80123^{commit}' 2>/dev/null; then
		case " $(tr '\n' ' ' <"$out_dir/unreachable-commits.txt" 2>/dev/null || true) " in
			*" 3f80123 "*) ;;
			*)
				echo "3f80123" >>"$out_dir/unreachable-commits.txt"
				dest="$out_dir/commits/3f80123"
				mkdir -p "$dest/tree"
				git -C "$repo" cat-file -p 3f80123 >"$dest/commit-object.txt"
				git -C "$repo" log -1 --format=fuller 3f80123 >"$dest/commit-message.txt"
				git -C "$repo" archive 3f80123 2>/dev/null | tar -x -C "$dest/tree" 2>/dev/null || true
				echo "3f80123 commit materialized explicitly (design-required)" >>"$disposition"
				;;
		esac
	fi
}

validate_gitleaks_report() {
	report="$1"
	label="$2"
	[ -f "$report" ] || die "$label: gitleaks report missing: $report"
	python3 - "$report" "$label" <<'PY'
import json, sys
path, label = sys.argv[1], sys.argv[2]
try:
    data = json.load(open(path))
except Exception as exc:
    raise SystemExit(f"{label}: invalid gitleaks JSON: {exc}")
if not isinstance(data, list):
    raise SystemExit(f"{label}: gitleaks JSON must be a list")
print(len(data))
PY
}

run_gitleaks_git() {
	source="$1"
	report="$2"
	log_opts="${3:---all}"
	stderr_log="${report%.json}.stderr"
	set +e
	gitleaks detect \
		--source "$source" \
		--config "$GITLEAKS_CONFIG" \
		--log-opts="$log_opts" \
		--report-format json \
		--report-path "$report" \
		--redact=100 \
		2>"$stderr_log"
	status=$?
	set -e
	echo "$status" >"${report%.json}.exit"
	if [ ! -f "$report" ]; then
		[ -s "$stderr_log" ] && cat "$stderr_log" >&2
		die "gitleaks git scan failed without report for $source (status=$status)"
	fi
	finding_count="$(validate_gitleaks_report "$report" "gitleaks-git:$source")"
	if [ "$finding_count" -gt 0 ]; then
		echo "gitleaks findings in $source: $finding_count (see $report)" >&2
		return 1
	fi
	if [ "$status" -ne 0 ]; then
		[ -s "$stderr_log" ] && cat "$stderr_log" >&2
		die "gitleaks git scan operational failure for $source (status=$status)"
	fi
	return 0
}

run_gitleaks_dir() {
	source="$1"
	report="$2"
	stderr_log="${report%.json}.stderr"
	[ -d "$source" ] || die "gitleaks dir scan source missing: $source"
	set +e
	gitleaks detect \
		--source "$source" \
		--config "$GITLEAKS_CONFIG" \
		--no-git \
		--report-format json \
		--report-path "$report" \
		--redact=100 \
		2>"$stderr_log"
	status=$?
	set -e
	echo "$status" >"${report%.json}.exit"
	if [ ! -f "$report" ]; then
		[ -s "$stderr_log" ] && cat "$stderr_log" >&2
		die "gitleaks dir scan failed without report for $source (status=$status)"
	fi
	finding_count="$(validate_gitleaks_report "$report" "gitleaks-dir:$source")"
	if [ "$finding_count" -gt 0 ]; then
		echo "gitleaks findings in $source: $finding_count (see $report)" >&2
		return 1
	fi
	if [ "$status" -ne 0 ]; then
		[ -s "$stderr_log" ] && cat "$stderr_log" >&2
		die "gitleaks dir scan operational failure for $source (status=$status)"
	fi
	return 0
}

summarize_gitleaks() {
	report="$1"
	summary="$2"
	finding_count="$(validate_gitleaks_report "$report" "summarize")"
	{
		echo "finding_count=$finding_count"
		python3 - "$report" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
for item in data:
    print(" | ".join(str(item.get(k, "")) for k in ("RuleID", "File", "StartLine")))
PY
	} >"$summary"
}

audit_local() {
	local_dir="$EVIDENCE_DIR/local"
	mkdir -p "$local_dir"

	inventory_refs "$REPO_ROOT" "$local_dir/refs-manifest.txt"
	url="$(resolve_remote_url)"
	remote_ls_refs "$url" "$local_dir/remote-ls-refs.txt"
	materialize_unreachable "$REPO_ROOT" "$local_dir/materialized-unreachable" "$local_dir/unreachable-disposition.txt"

	run_gitleaks_git "$REPO_ROOT" "$local_dir/gitleaks-reachable.json" "--all"
	summarize_gitleaks "$local_dir/gitleaks-reachable.json" "$local_dir/gitleaks-reachable.summary.txt"

	if [ -d "$local_dir/materialized-unreachable" ] && [ -n "$(find "$local_dir/materialized-unreachable" -type f 2>/dev/null | head -1)" ]; then
		run_gitleaks_dir "$local_dir/materialized-unreachable" "$local_dir/gitleaks-unreachable.json"
		summarize_gitleaks "$local_dir/gitleaks-unreachable.json" "$local_dir/gitleaks-unreachable.summary.txt"
	fi
}

resolve_remote_url() {
	if [ -n "${REMOTE_URL:-}" ]; then
		printf '%s' "$REMOTE_URL"
		return
	fi
	git -C "$REPO_ROOT" remote get-url "$REMOTE_NAME"
}

audit_remote() {
	remote_dir="$EVIDENCE_DIR/remote"
	mirror="$EVIDENCE_DIR/remote-mirror.git"
	mkdir -p "$remote_dir"
	url="$(resolve_remote_url)"
	ref_manifest="$remote_dir/advertised-refs.txt"

	remote_ls_refs "$url" "$ref_manifest"
	rm -rf "$mirror"
	git clone --mirror "$url" "$mirror"

	# Mirror clone may omit pull refs; fetch advertised namespaces and fail on transport errors.
	set +e
	git -C "$mirror" fetch "$url" \
		'+refs/heads/*:refs/heads/*' \
		'+refs/tags/*:refs/tags/*' \
		'+refs/pull/*/head:refs/pull/*/head' \
		'+refs/pull/*/merge:refs/pull/*/merge' \
		>>"$remote_dir/fetch.log" 2>&1
	fetch_status=$?
	set -e
	if [ "$fetch_status" -ne 0 ]; then
		cat "$remote_dir/fetch.log" >&2
		die "remote mirror fetch failed (status=$fetch_status)"
	fi

	# Verify every advertised ref is present in the mirror inventory.
	mirror_inventory="$remote_dir/mirror-inventory.txt"
	inventory_refs "$mirror" "$mirror_inventory"
	missing_ref="$(awk '
		NR==FNR { mirror[$1]=1; next }
		$1 ~ /\^\{\}$/ { next }
		!($1 in mirror) { print $1; exit }
	' "$mirror_inventory" "$ref_manifest")"
	[ -z "$missing_ref" ] || die "advertised ref missing from mirror: $missing_ref"

	inventory_refs "$mirror" "$remote_dir/refs-manifest.txt"
	git -C "$mirror" for-each-ref --format='%(refname) %(objectname)' refs/heads \
		| awk '$1 != "refs/heads/main" {print}' \
		>"$remote_dir/stale-head-candidates.txt"

	run_gitleaks_git "$mirror" "$remote_dir/gitleaks-remote-all.json" "--all"
	summarize_gitleaks "$remote_dir/gitleaks-remote-all.json" "$remote_dir/gitleaks-remote-all.summary.txt"
}

normalize_remote_refs() {
	in="$1"
	out="$2"
	awk '
		$1 == "refs/heads/HEAD" { next }
		$1 ~ /\^\{\}$/ { next }
		{ print }
	' "$in" | sort -u >"$out"
}

compare_manifests() {
	local_manifest_raw="$EVIDENCE_DIR/local/remote-ls-refs.txt"
	remote_manifest_raw="$EVIDENCE_DIR/remote/refs-manifest.txt"
	local_manifest="$EVIDENCE_DIR/local/remote-ls-refs.normalized.txt"
	remote_manifest="$EVIDENCE_DIR/remote/refs-manifest.normalized.txt"
	compare_out="$EVIDENCE_DIR/ref-drift.txt"
	url="$(resolve_remote_url)"
	remote_ls_refs "$url" "$local_manifest_raw"
	[ -f "$remote_manifest_raw" ] || die "missing remote manifest for drift compare"
	normalize_remote_refs "$local_manifest_raw" "$local_manifest"
	normalize_remote_refs "$remote_manifest_raw" "$remote_manifest"
	if diff -u "$local_manifest" "$remote_manifest" >"$compare_out"; then
		: >"$compare_out"
	fi
}

collect_failures() {
	fail=0
	for summary in \
		"$EVIDENCE_DIR/local/gitleaks-reachable.summary.txt" \
		"$EVIDENCE_DIR/local/gitleaks-unreachable.summary.txt" \
		"$EVIDENCE_DIR/remote/gitleaks-remote-all.summary.txt"
	do
		[ -f "$summary" ] || continue
		count="$(awk -F= '/^finding_count=/{print $2}' "$summary" | tail -1)"
		if [ -n "$count" ] && [ "$count" != "0" ]; then
			echo "FAIL: $summary reports finding_count=$count" >&2
			fail=1
		fi
	done

	for report in \
		"$EVIDENCE_DIR/local/gitleaks-reachable.json" \
		"$EVIDENCE_DIR/local/gitleaks-unreachable.json" \
		"$EVIDENCE_DIR/remote/gitleaks-remote-all.json"
	do
		[ -f "$report" ] || continue
		if ! validate_gitleaks_report "$report" "final-check" >/dev/null 2>&1; then
			echo "FAIL: invalid or stale gitleaks report: $report" >&2
			fail=1
		fi
	done

	if [ -f "$EVIDENCE_DIR/ref-drift.txt" ] && [ -s "$EVIDENCE_DIR/ref-drift.txt" ]; then
		if grep -qE '^[<>+-]' "$EVIDENCE_DIR/ref-drift.txt"; then
			echo "FAIL: ref drift detected; see $EVIDENCE_DIR/ref-drift.txt" >&2
			fail=1
		fi
	fi
	return "$fail"
}

main() {
	case "$MODE" in
	local | remote | both) ;;
	-h | --help | help) usage; exit 0 ;;
	*) usage; die "unknown mode: ${MODE:-<empty>}" ;;
	esac

	require_cmd git
	require_cmd python3
	prepare_evidence_dir
	write_header
	validate_gitleaks_config

	case "$MODE" in
	local) audit_local ;;
	remote) audit_remote ;;
	both)
		audit_local
		audit_remote
		compare_manifests
		;;
	esac

	set +e
	collect_failures
	failures=$?
	set -e
	if [ "$failures" -ne 0 ]; then
		echo "audit failed; evidence in $EVIDENCE_DIR" >&2
		exit 1
	fi
	echo "audit passed; evidence in $EVIDENCE_DIR"
}

main "$@"
