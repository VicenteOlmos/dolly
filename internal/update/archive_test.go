package update

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

func TestParseChecksumsMissingEntry(t *testing.T) {
	asset := "dolly_linux_x86_64.tar.gz"
	other := strings.Repeat("a", 64) + "  other.txt"
	_, err := parseChecksums([]byte(other), asset)
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyArchiveSHA256Mismatch(t *testing.T) {
	err := verifyArchiveSHA256([]byte("data"), strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractValidArchive(t *testing.T) {
	content := []byte("#!/bin/sh\necho dolly\n")
	archive := buildTarGz(t, "dolly", content)

	stageDir := t.TempDir()
	path, sha, err := extractAndStage(archive, "dolly_linux_x86_64.tar.gz", "linux", stageDir)
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
	}{
		{
			name:    "tar absolute",
			archive: buildTarGz(t, "/dolly", []byte("bad")),
		},
		{
			name:    "tar traversal",
			archive: buildTarGz(t, "../dolly", []byte("bad")),
		},
		{
			name:    "tar nested",
			archive: buildTarGz(t, "bin/dolly", []byte("bad")),
		},
		{
			name:    "tar symlink",
			archive: buildTarGzTyped(t, "dolly", tar.TypeSymlink, nil),
		},
		{
			name:    "tar device",
			archive: buildTarGzTyped(t, "dolly", tar.TypeChar, nil),
		},
		{
			name: "tar extra member",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: []byte("ok")},
				{name: "extra.txt", typ: tar.TypeReg, content: []byte("bad")},
			}),
		},
		{
			name: "tar duplicate executable",
			archive: buildTarGzMulti(t, []tarEntry{
				{name: "dolly", typ: tar.TypeReg, content: []byte("one")},
				{name: "dolly", typ: tar.TypeReg, content: []byte("two")},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := extractAndStage(tt.archive, "dolly_linux_x86_64.tar.gz", "linux", t.TempDir())
			if err == nil {
				t.Fatal("expected extraction failure")
			}
		})
	}
}

func TestExtractRejectsValidBinaryPlusExtraMembers(t *testing.T) {
	valid := []byte("#!/bin/sh\necho ok\n")
	assertRejected := func(t *testing.T, archive []byte) {
		t.Helper()
		dir := t.TempDir()
		target := writeTestBinary(t, dir, "dolly")
		before := fileSHA256(t, target)
		_, _, err := extractAndStage(archive, "dolly_linux_x86_64.tar.gz", "linux", dir)
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
		t.Run(tc.name, func(t *testing.T) {
			assertRejected(t, tc.archive)
		})
	}
}

func writeTestBinary(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
