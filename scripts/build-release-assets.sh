#!/bin/sh
set -eu

OUT_DIR="${1:-dist}"
VERSION="${VERSION:-}"
COMMIT="${COMMIT:-}"
DATE="${DATE:-}"

if [ -z "$VERSION" ]; then
	tag="$(git describe --tags --exact-match 2>/dev/null || true)"
	case "$tag" in
		v*) VERSION="${tag#v}" ;;
		*) VERSION="0.0.0-local" ;;
	esac
fi

if [ -z "$COMMIT" ]; then
	COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo local)"
fi

if [ -z "$DATE" ]; then
	DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

LDFLAGS="-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"

if command -v sha256sum >/dev/null 2>&1; then
	CHECKSUM_CMD=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	CHECKSUM_CMD="shasum -a 256"
else
	echo "error: sha256sum or shasum is required" >&2
	exit 1
fi

mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"
staging="$(mktemp -d "${TMPDIR:-/tmp}/dolly-release.XXXXXX")"
trap 'rm -rf "$staging"' EXIT INT HUP TERM

build_target() {
	goos="$1"
	goarch="$2"
	arch_name="$3"
	binary_name="$4"
	archive_name="$5"

	work="$staging/$archive_name"
	mkdir -p "$work"
	GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -ldflags "$LDFLAGS" -o "$work/$binary_name" ./cmd/dolly
	chmod 755 "$work/$binary_name"

	case "$archive_name" in
		*.zip)
			(
				cd "$work"
				if command -v zip >/dev/null 2>&1; then
					zip -q "$OUT_DIR/$archive_name" "$binary_name"
				elif command -v python3 >/dev/null 2>&1; then
					python3 -c "import zipfile,sys; z=zipfile.ZipFile(sys.argv[1],'w',zipfile.ZIP_DEFLATED); z.write(sys.argv[2], sys.argv[2]); z.close()" "$OUT_DIR/$archive_name" "$binary_name"
				else
					echo "error: zip or python3 is required for Windows archives" >&2
					exit 1
				fi
			)
			;;
		*.tar.gz)
			tar -czf "$OUT_DIR/$archive_name" -C "$work" "$binary_name"
			;;
		*)
			echo "error: unknown archive type: $archive_name" >&2
			exit 1
			;;
	esac
}

build_target linux amd64 x86_64 dolly dolly_linux_x86_64.tar.gz
build_target linux arm64 arm64 dolly dolly_linux_arm64.tar.gz
build_target darwin amd64 x86_64 dolly dolly_darwin_x86_64.tar.gz
build_target darwin arm64 arm64 dolly dolly_darwin_arm64.tar.gz
build_target windows amd64 x86_64 dolly.exe dolly_windows_x86_64.zip
build_target windows arm64 arm64 dolly.exe dolly_windows_arm64.zip

(
	cd "$OUT_DIR"
	# shellcheck disable=SC2086
	$CHECKSUM_CMD dolly_*.tar.gz dolly_*.zip > checksums.txt
)

echo "Release assets written to $OUT_DIR"
