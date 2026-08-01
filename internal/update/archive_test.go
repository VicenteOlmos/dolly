package update

import (
	"archive/tar"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	asset := "dolly_linux_x86_64.tar.gz"
	validHash := strings.Repeat("a", 64)
	otherHash := strings.Repeat("b", 64)
	valid := strings.Join([]string{
		otherHash + "  other.txt",
		validHash + "  " + asset,
	}, "\n")

	got, err := parseChecksums([]byte(valid), asset)
	if err != nil {
		t.Fatalf("parseChecksums: %v", err)
	}
	if got != validHash {
		t.Fatalf("got %q", got)
	}

	_, err = parseChecksums([]byte(valid+"\n"+valid), asset)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate err = %v", err)
	}

	short := strings.Repeat("c", 32)
	_, err = parseChecksums([]byte(short+"  "+asset), asset)
	if err == nil || !strings.Contains(err.Error(), "64 hex") {
		t.Fatalf("short hash err = %v", err)
	}

	badHex := strings.Repeat("z", 64)
	_, err = parseChecksums([]byte(badHex+"  "+asset), asset)
	if err == nil || !strings.Contains(err.Error(), "valid hex") {
		t.Fatalf("bad hex err = %v", err)
	}
}

func TestExtractValidArchive(t *testing.T) {
	content := []byte("#!/bin/sh\necho dolly\n")
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	var archive []byte
	execName := ExecutableBaseName(runtimeGOOS())
	if strings.HasSuffix(assetName, ".zip") {
		archive = buildZip(t, execName, content)
	} else {
		archive = buildTarGz(t, execName, content)
	}

	stageDir := t.TempDir()
	path, sha, err := extractAndStage(archive, assetName, runtimeGOOS(), stageDir)
	if err != nil {
		t.Fatalf("extractAndStage: %v", err)
	}
	if path == "" || sha == "" {
		t.Fatal("expected staged path and sha")
	}
	if path != candidatePath(stageDir) {
		t.Fatalf("staged path = %q, want %q", path, candidatePath(stageDir))
	}
}

func TestWriteCandidateExclusiveRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := writeCandidateExclusive(dir, []byte("one")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writeCandidateExclusive(dir, []byte("two")); err == nil {
		t.Fatal("expected exclusive create failure")
	}
}

func TestExtractHostileArchive(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		asset   string
		goos    string
	}{
		{
			name:    "tar absolute",
			archive: buildTarGz(t, "/dolly", []byte("bad")),
			asset:   "dolly_linux_x86_64.tar.gz",
			goos:    "linux",
		},
		{
			name:    "tar traversal",
			archive: buildTarGz(t, "../dolly", []byte("bad")),
			asset:   "dolly_linux_x86_64.tar.gz",
			goos:    "linux",
		},
		{
			name:    "tar nested",
			archive: buildTarGz(t, "bin/dolly", []byte("bad")),
			asset:   "dolly_linux_x86_64.tar.gz",
			goos:    "linux",
		},
		{
			name:    "zip traversal",
			archive: buildZip(t, "../dolly.exe", []byte("bad")),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "tar symlink",
			archive: buildTarGzTyped(t, "dolly", tar.TypeSymlink, nil),
			asset:   "dolly_linux_x86_64.tar.gz",
			goos:    "linux",
		},
		{
			name:    "tar device",
			archive: buildTarGzTyped(t, "dolly", tar.TypeChar, nil),
			asset:   "dolly_linux_x86_64.tar.gz",
			goos:    "linux",
		},
		{
			name: "tar extra member",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: []byte("ok")},
				{name: "extra.txt", typ: tar.TypeReg, content: []byte("bad")},
			}),
			asset: "dolly_linux_x86_64.tar.gz",
			goos:  "linux",
		},
		{
			name:    "zip backslash traversal",
			archive: buildZip(t, "..\\dolly.exe", []byte("bad")),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip windows drive",
			archive: buildZip(t, "C:\\dolly.exe", []byte("bad")),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip directory executable name",
			archive: buildZipWithMode(t, "dolly.exe/", os.ModeDir|0o755, nil),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip symlink executable name",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeSymlink|0o777, []byte("target")),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip char device executable name",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeDevice|0o200000|0o666, nil),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip block device executable name",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeDevice|0o666, nil),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip named pipe executable name",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeNamedPipe|0o666, nil),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
		{
			name:    "zip socket executable name",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeSocket|0o666, nil),
			asset:   "dolly_windows_x86_64.zip",
			goos:    "windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := extractAndStage(tt.archive, tt.asset, tt.goos, t.TempDir())
			if err == nil {
				t.Fatal("expected extraction failure")
			}
		})
	}
}

func TestExtractRejectsWindowsZipSpecialMembers(t *testing.T) {
	cases := []struct {
		name    string
		archive []byte
	}{
		{
			name:    "directory",
			archive: buildZipWithMode(t, "dolly.exe/", os.ModeDir|0o755, nil),
		},
		{
			name:    "symlink",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeSymlink|0o777, []byte("target")),
		},
		{
			name:    "device",
			archive: buildZipWithMode(t, "dolly.exe", os.ModeDevice|0o666, nil),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stageDir := t.TempDir()
			_, _, err := extractAndStage(tc.archive, "dolly_windows_x86_64.zip", "windows", stageDir)
			if err == nil {
				t.Fatal("expected extraction failure")
			}
			if _, err := os.Stat(candidatePath(stageDir)); !os.IsNotExist(err) {
				t.Fatal("candidate staged for hostile zip entry")
			}
		})
	}
}

func TestExtractHostileWindowsZipLeavesTargetUnchanged(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	before := fileSHA256(t, target)
	archive := buildZipWithMode(t, "dolly.exe", os.ModeDevice|0o666, nil)

	_, _, err := extractAndStage(archive, "dolly_windows_x86_64.zip", "windows", dir)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err = %v", err)
	}
	if after := fileSHA256(t, target); after != before {
		t.Fatal("target mutated on hostile zip extraction")
	}
	if _, err := os.Stat(candidatePath(dir)); !os.IsNotExist(err) {
		t.Fatal("candidate staged for hostile zip entry")
	}
}

func TestExtractRejectsValidBinaryPlusExtraMembers(t *testing.T) {
	valid := []byte("#!/bin/sh\necho ok\n")
	assertRejected := func(t *testing.T, archive []byte, asset, goos, execName string) {
		t.Helper()
		dir := t.TempDir()
		target := writeFakeBinary(t, dir, execName, 0o755)
		before := fileSHA256(t, target)
		_, _, err := extractAndStage(archive, asset, goos, dir)
		if err == nil {
			t.Fatal("expected extraction failure")
		}
		if after := fileSHA256(t, target); after != before {
			t.Fatal("target mutated on rejected archive")
		}
		if _, err := os.Stat(candidatePath(dir)); !os.IsNotExist(err) {
			t.Fatal("candidate staged for rejected archive")
		}
	}

	tarCases := []struct {
		name    string
		archive []byte
	}{
		{
			name: "extra directory",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeDir},
			}),
		},
		{
			name: "extra symlink",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeSymlink, linkname: "dolly"},
			}),
		},
		{
			name: "extra hard link",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeLink, linkname: "dolly"},
			}),
		},
		{
			name: "extra char device",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeChar},
			}),
		},
		{
			name: "extra block device",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeBlock},
			}),
		},
		{
			name: "extra fifo",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: valid},
				{name: "extra", typ: tar.TypeFifo},
			}),
		},
	}
	for _, tc := range tarCases {
		t.Run("tar/"+tc.name, func(t *testing.T) {
			assertRejected(t, tc.archive, "dolly_linux_x86_64.tar.gz", "linux", "dolly")
		})
	}

	zipCases := []struct {
		name    string
		archive []byte
	}{
		{
			name: "extra directory",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra/", mode: os.ModeDir | 0o755},
			}),
		},
		{
			name: "extra symlink",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra", mode: os.ModeSymlink | 0o777, content: []byte("dolly.exe")},
			}),
		},
		{
			name: "extra char device",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra", mode: os.ModeDevice | 0o200000 | 0o666},
			}),
		},
		{
			name: "extra block device",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra", mode: os.ModeDevice | 0o666},
			}),
		},
		{
			name: "extra named pipe",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra", mode: os.ModeNamedPipe | 0o666},
			}),
		},
		{
			name: "extra socket",
			archive: buildZipMulti(t, []zipEntry{
				{name: "dolly.exe", mode: 0o755, content: valid},
				{name: "extra", mode: os.ModeSocket | 0o666},
			}),
		},
	}
	for _, tc := range zipCases {
		t.Run("zip/"+tc.name, func(t *testing.T) {
			assertRejected(t, tc.archive, "dolly_windows_x86_64.zip", "windows", "dolly.exe")
		})
	}
}

func runtimeGOOS() string {
	asset, err := CurrentAsset()
	if err != nil {
		return runtime.GOOS
	}
	if strings.HasSuffix(asset, ".zip") {
		return "windows"
	}
	return runtime.GOOS
}
