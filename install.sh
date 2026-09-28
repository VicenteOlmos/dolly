#!/bin/sh
set -eu

DOLLY_VERSION="${DOLLY_VERSION:-latest}"
DOLLY_INSTALL_DIR="${DOLLY_INSTALL_DIR:-/usr/local/bin}"
DOLLY_REPO="${DOLLY_REPO:-VicenteOlmos/dolly}"
DOLLY_DOWNLOAD_TIMEOUT="${DOLLY_DOWNLOAD_TIMEOUT:-60}"

die() {
	printf '%s\n' "error: $*" >&2
	exit 1
}

warn() {
	printf '%s\n' "warning: $*" >&2
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

download() {
	url="$1"
	output="$2"

	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --connect-timeout 10 --max-time "$DOLLY_DOWNLOAD_TIMEOUT" "$url" -o "$output"
	elif command -v wget >/dev/null 2>&1; then
		wget -q --timeout="$DOLLY_DOWNLOAD_TIMEOUT" "$url" -O "$output"
	else
		die "curl or wget is required"
	fi
}

cleanup() {
	if [ -n "${tmp_target:-}" ]; then
		rm -f "$tmp_target" || true
	fi
	if [ -n "${sudo_tmp_target:-}" ]; then
		sudo rm -f "$sudo_tmp_target" || true
	fi
	if [ -n "${tmpdir:-}" ]; then
		rm -rf "$tmpdir"
	fi
}

# test hook: when DOLLY_MOCK_DOWNLOAD_DIR is set, copy assets from
# that directory instead of fetching from the network.  Used by
# test/install-behavior.sh so checksum-policy tests run without
# network calls.
if [ -n "${DOLLY_MOCK_DOWNLOAD_DIR:-}" ]; then
	download() {
		fname="${1##*/}"
		src="$DOLLY_MOCK_DOWNLOAD_DIR/$fname"
		if [ -f "$src" ]; then
			cp "$src" "$2"
		else
			return 1
		fi
	}
fi

case "$DOLLY_REPO" in
	*/*)
		;;
	*)
		die "DOLLY_REPO must use GitHub owner/repo format, got: $DOLLY_REPO"
		;;
esac

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
	linux|darwin)
		;;
	*)
		die "unsupported OS: $os"
		;;
esac

machine="$(uname -m)"
case "$machine" in
	x86_64|amd64)
		arch="x86_64"
		;;
	arm64|aarch64)
		arch="arm64"
		;;
	*)
		die "unsupported architecture: $machine"
		;;
esac

need_cmd tar


clean_archive_name() {
	name="$1"
	name="${name#./}"
	name="${name%/}"
	printf '%s' "$name"
}

validate_archive_path_raw() {
	name="$1"
	case "$name" in
		""|".")
			die "archive contains unexpected entry $name"
			;;
	esac
	case "$name" in
		/*)
			die "archive absolute path rejected: $name"
			;;
	esac
	case "$name" in
		..|../*|*/..|*/../*)
			die "archive path traversal rejected: $name"
			;;
	esac
}

validate_tar_member_name() {
	raw_name="$1"
	want_name="$2"
	validate_archive_path_raw "$raw_name"
	clean="$(clean_archive_name "$raw_name")"
	case "$clean" in
		""|".")
			die "archive contains unexpected entry $raw_name"
			;;
	esac
	case "$clean" in
		..|../*|*/..|*/../*)
			die "archive path traversal rejected: $raw_name"
			;;
	esac
	case "$clean" in
		*/*)
			die "archive contains nested path $raw_name"
			;;
	esac
	if [ "$clean" != "$want_name" ]; then
		die "unexpected archive member $raw_name"
	fi
}

extract_validated_tar() {
	archive="$1"
	extract_dir="$2"
	want_name="dolly"
	found=0

	while IFS= read -r line; do
		[ -n "$line" ] || continue
		type="$(printf '%s' "$line" | cut -c1)"
		name="$(printf '%s' "$line" | awk '{
			for (i = 6; i <= NF; i++) {
				if (i > 6) {
					printf " "
				}
				printf "%s", $i
			}
		}')"
		case "$name" in
			*" -> "*)
				name="${name%% -> *}"
				;;
		esac
		validate_tar_member_name "$name" "$want_name"
		case "$type" in
			-)
				;;
			*)
				die "archive selected entry $name is not a regular file"
				;;
		esac
		found=$((found + 1))
	done <<EOF
$(tar -tvf "$archive")
EOF

	if [ "$found" -ne 1 ]; then
		die "archive did not contain exactly one $want_name binary"
	fi

	mkdir -p "$extract_dir"
	tar -xzf "$archive" -C "$extract_dir" "$want_name"
}

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/dolly-install.XXXXXX")"
trap cleanup EXIT INT HUP TERM

asset_name="dolly_${os}_${arch}.tar.gz"
archive="$tmpdir/$asset_name"
checksums="$tmpdir/checksums.txt"

if [ "$DOLLY_VERSION" = "latest" ]; then
	base_url="https://github.com/$DOLLY_REPO/releases/latest/download"
else
	version="${DOLLY_VERSION#v}"
	[ -n "$version" ] || die "DOLLY_VERSION cannot be empty"
	base_url="https://github.com/$DOLLY_REPO/releases/download/v$version"
fi

asset_url="$base_url/$asset_name"

printf '%s\n' "Downloading $asset_url"
download "$asset_url" "$archive"

if download "$base_url/checksums.txt" "$checksums"; then
	checksum_line="$(awk -v file="$asset_name" '$NF == file { print; exit }' "$checksums")"
	if [ -n "$checksum_line" ]; then
		expected_sha="$(printf '%s\n' "$checksum_line" | awk '{ print $1 }')"
		if command -v sha256sum >/dev/null 2>&1; then
			( cd "$tmpdir" && printf '%s  %s\n' "$expected_sha" "$asset_name" | sha256sum -c - )
		elif command -v shasum >/dev/null 2>&1; then
			( cd "$tmpdir" && printf '%s  %s\n' "$expected_sha" "$asset_name" | shasum -a 256 -c - )
		elif command -v openssl >/dev/null 2>&1; then
			actual_sha="$(openssl dgst -sha256 "$archive" | awk '{ print $NF }' | tr '[:upper:]' '[:lower:]')"
			expected_lower="$(printf '%s' "$expected_sha" | tr '[:upper:]' '[:lower:]')"
			[ "$actual_sha" = "$expected_lower" ] || die "checksum mismatch for $asset_name"
		elif [ "$DOLLY_VERSION" != "latest" ]; then
			die "checksum verification required for tagged release but sha256sum, shasum, or openssl is not available"
		elif [ "${DOLLY_ALLOW_UNVERIFIED:-}" = "1" ]; then
			warn "checksums.txt was found but sha256sum, shasum, or openssl is required to verify; skipping checksum verification (DOLLY_ALLOW_UNVERIFIED=1)"
		else
			die "checksum verification required: sha256sum, shasum, or openssl is not available (set DOLLY_ALLOW_UNVERIFIED=1 to skip)"
		fi
	elif [ "$DOLLY_VERSION" != "latest" ]; then
		die "checksum verification required for tagged release but $asset_name is not listed in checksums.txt"
	elif [ "${DOLLY_ALLOW_UNVERIFIED:-}" = "1" ]; then
		warn "checksums.txt does not contain $asset_name; skipping checksum verification (DOLLY_ALLOW_UNVERIFIED=1)"
	else
		die "checksum verification required: $asset_name is not listed in checksums.txt (set DOLLY_ALLOW_UNVERIFIED=1 to skip)"
	fi
elif [ "$DOLLY_VERSION" != "latest" ]; then
	die "checksum verification required for tagged release but checksums.txt could not be downloaded"
elif [ "${DOLLY_ALLOW_UNVERIFIED:-}" = "1" ]; then
	warn "checksums.txt was not found; checksum verification skipped (DOLLY_ALLOW_UNVERIFIED=1)"
else
	die "checksum verification required: checksums.txt could not be downloaded (set DOLLY_ALLOW_UNVERIFIED=1 to skip)"
fi

extract_dir="$tmpdir/extract"
extract_validated_tar "$archive" "$extract_dir"
binary_path="$extract_dir/dolly"
[ -f "$binary_path" ] || die "archive did not contain a dolly binary"
chmod 755 "$binary_path"

target="$DOLLY_INSTALL_DIR/dolly"

if [ "$DOLLY_INSTALL_DIR" = "/usr/local/bin" ] && [ ! -w "/usr/local/bin" ]; then
	printf '%s\n' "Install dir $DOLLY_INSTALL_DIR requires sudo. This will prompt for your password." >&2
	printf '%s\n' "To install without sudo, set DOLLY_INSTALL_DIR to a user-writable dir (e.g. ~/.local/bin)." >&2
fi

if [ -d "$DOLLY_INSTALL_DIR" ] && [ -w "$DOLLY_INSTALL_DIR" ]; then
	tmp_target="$(mktemp "$DOLLY_INSTALL_DIR/.dolly.XXXXXX")"
	cp "$binary_path" "$tmp_target"
	chmod 755 "$tmp_target"
	mv -f "$tmp_target" "$target"
	tmp_target=""
elif mkdir -p "$DOLLY_INSTALL_DIR" 2>/dev/null && [ -w "$DOLLY_INSTALL_DIR" ]; then
	tmp_target="$(mktemp "$DOLLY_INSTALL_DIR/.dolly.XXXXXX")"
	cp "$binary_path" "$tmp_target"
	chmod 755 "$tmp_target"
	mv -f "$tmp_target" "$target"
	tmp_target=""
elif [ ! -w "$DOLLY_INSTALL_DIR" ] && [ "$DOLLY_INSTALL_DIR" != "/usr/local/bin" ]; then
	die "$DOLLY_INSTALL_DIR is not writable and is not the default; set DOLLY_INSTALL_DIR to a writable directory"
else
	command -v sudo >/dev/null 2>&1 || die "$DOLLY_INSTALL_DIR is not writable and sudo is not available"
	sudo mkdir -p "$DOLLY_INSTALL_DIR"
	sudo_tmp_target="$DOLLY_INSTALL_DIR/.dolly.$$"
	sudo cp "$binary_path" "$sudo_tmp_target"
	sudo chmod 755 "$sudo_tmp_target"
	sudo mv -f "$sudo_tmp_target" "$target"
	sudo_tmp_target=""
fi

printf '%s\n' "Installed dolly to $target"
if "$target" version >/dev/null 2>&1; then
	"$target" version
else
	warn "installed binary did not print version successfully"
fi
