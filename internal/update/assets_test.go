package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseAssetContract(t *testing.T) {
	releaseYML, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	installSH, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	installPS1, err := os.ReadFile(filepath.Join("..", "..", "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	buildScript, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-release-assets.sh"))
	if err != nil {
		t.Fatalf("read build-release-assets.sh: %v", err)
	}

	for _, name := range ReleaseAssetNames() {
		for _, blob := range []struct {
			label string
			data  []byte
		}{
			{"release.yml", releaseYML},
			{"build-release-assets.sh", buildScript},
		} {
			if !strings.Contains(string(blob.data), name) {
				t.Fatalf("%s missing asset %q", blob.label, name)
			}
		}
	}

	// install.sh/install.ps1 build asset names dynamically; verify patterns instead.
	if !strings.Contains(string(installSH), `asset_name="dolly_${os}_${arch}.tar.gz"`) {
		t.Fatalf("install.sh asset naming pattern changed")
	}
	if !strings.Contains(string(installPS1), `dolly_windows_${arch}.zip`) {
		t.Fatalf("install.ps1 windows asset naming pattern changed")
	}
	for _, name := range []string{"checksums.txt", "tar.gz"} {
		if !strings.Contains(string(installSH), name) {
			t.Fatalf("install.sh missing %q reference", name)
		}
	}

	if got := len(ReleaseAssetNames()); got != 7 {
		t.Fatalf("ReleaseAssetNames len = %d, want 7", got)
	}

	pairs := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "dolly_linux_x86_64.tar.gz"},
		{"linux", "arm64", "dolly_linux_arm64.tar.gz"},
		{"darwin", "amd64", "dolly_darwin_x86_64.tar.gz"},
		{"darwin", "arm64", "dolly_darwin_arm64.tar.gz"},
		{"windows", "amd64", "dolly_windows_x86_64.zip"},
		{"windows", "arm64", "dolly_windows_arm64.zip"},
	}
	for _, tt := range pairs {
		got, err := AssetName(tt.goos, tt.goarch)
		if err != nil {
			t.Fatalf("AssetName(%s,%s): %v", tt.goos, tt.goarch, err)
		}
		if got != tt.want {
			t.Fatalf("AssetName(%s,%s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}

	unsupported := []struct{ goos, goarch string }{
		{"freebsd", "amd64"},
		{"linux", "386"},
		{"windows", "mips"},
	}
	for _, tt := range unsupported {
		if _, err := AssetName(tt.goos, tt.goarch); err == nil {
			t.Fatalf("AssetName(%s,%s) should fail", tt.goos, tt.goarch)
		}
	}

	if _, err := CurrentAsset(); err != nil {
		t.Fatalf("CurrentAsset: %v", err)
	}
	if runtime.GOOS == "windows" {
		if got := ExecutableBaseName("windows"); got != "dolly.exe" {
			t.Fatalf("ExecutableBaseName(windows) = %q", got)
		}
	} else if got := ExecutableBaseName(runtime.GOOS); got != "dolly" {
		t.Fatalf("ExecutableBaseName(%s) = %q", runtime.GOOS, got)
	}
}

func buildTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildCurrentArchive(t *testing.T, content []byte) []byte {
	t.Helper()
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	execName := ExecutableBaseName(runtime.GOOS)
	if strings.HasSuffix(assetName, ".zip") {
		return buildZip(t, execName, content)
	}
	return buildTarGz(t, execName, content)
}

func buildZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	return buildZipWithMode(t, name, 0o755, content)
}

func buildZipWithMode(t *testing.T, name string, mode os.FileMode, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	hdr := &zip.FileHeader{Name: name}
	if mode == 0 {
		mode = 0o755
	}
	hdr.SetMode(mode)
	if mode.IsRegular() {
		hdr.Method = zip.Deflate
	}
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 0 {
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name     string
	typ      byte
	content  []byte
	linkname string
}

func buildTarGzTyped(t *testing.T, name string, typ byte, content []byte) []byte {
	t.Helper()
	return buildTarGzMulti(t, []tarEntry{{name: name, typ: typ, content: content}})
}

func buildTarGzMulti(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o755, Typeflag: e.typ, Linkname: e.linkname}
		if e.typ == tar.TypeReg || e.typ == tar.TypeRegA {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if len(e.content) > 0 {
			if _, err := tw.Write(e.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type zipEntry struct {
	name    string
	mode    os.FileMode
	content []byte
}

func buildZipMulti(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o755
		}
		hdr := &zip.FileHeader{Name: e.name}
		hdr.SetMode(mode)
		if mode.IsRegular() {
			hdr.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if len(e.content) > 0 {
			if _, err := w.Write(e.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func checksumLine(assetName string, data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "  " + assetName
}
