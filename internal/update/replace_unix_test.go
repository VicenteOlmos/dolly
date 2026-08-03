//go:build !windows

package update

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpdatedWithCopiedExecutableE2E(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dolly")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	beforeSHA := fileSHA256(t, target)

	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho copied-exe-e2e\n")
	archive := buildCurrentArchive(t, content)
	checksums := []byte(checksumLine(assetName, archive))

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusUpdated {
		t.Fatalf("status = %s, want updated", result.Status)
	}
	if after := fileSHA256(t, target); after == beforeSHA {
		t.Fatal("target not updated")
	}
}

func TestApplyReplacementPreservesExactUnixMode(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly", 0o750)
	oldSHA := fileSHA256(t, target)

	candidate := filepath.Join(dir, candidateBaseName)
	newContent := []byte("#!/bin/sh\necho mode-preserve\n")
	if err := os.WriteFile(candidate, newContent, 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA := fileSHA256(t, candidate)

	status, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: newSHA,
		OldSHA:       oldSHA,
		OldSize:      mustSize(t, target),
	})
	if err != nil {
		t.Fatalf("applyReplacement: %v", err)
	}
	if status != StatusUpdated {
		t.Fatalf("status = %s, want updated", status)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("updated mode = %04o, want 0750", got)
	}
}

func TestApplyReplacementFailureLeavesTargetUnchanged(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly", 0o750)
	beforeSHA := fileSHA256(t, target)
	beforeMode := mustPerm(t, target)

	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: strings.Repeat("a", 64),
		OldSHA:       beforeSHA,
		OldSize:      mustSize(t, target),
	})
	if err == nil {
		t.Fatal("expected digest mismatch failure")
	}
	if got := fileSHA256(t, target); got != beforeSHA {
		t.Fatal("target bytes changed on failure")
	}
	if got := mustPerm(t, target); got != beforeMode {
		t.Fatalf("target mode = %04o, want %04o", got, beforeMode)
	}
}

func mustPerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestRunUpdatedExecutesCopiedBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dolly")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	marker := "dolly-updated-exec-marker"
	content := []byte("#!/bin/sh\necho " + marker + "\n")
	archive := buildCurrentArchive(t, content)
	checksums := []byte(checksumLine(assetName, archive))

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusUpdated {
		t.Fatalf("status = %s, want updated", result.Status)
	}

	out, err := exec.Command(target).Output()
	if err != nil {
		t.Fatalf("execute updated binary: %v", err)
	}
	if !strings.Contains(string(out), marker) {
		t.Fatalf("output = %q, want marker %q", string(out), marker)
	}
}

func TestApplyReplacementWithCopiedExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dolly")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}

	oldSHA := fileSHA256(t, target)
	candidate := filepath.Join(dir, candidateBaseName)
	newContent := []byte("#!/bin/sh\necho copied-exe-replace\n")
	if err := os.WriteFile(candidate, newContent, 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA := fileSHA256(t, candidate)

	status, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: newSHA,
		OldSHA:       oldSHA,
		OldSize:      mustSize(t, target),
	})
	if err != nil {
		t.Fatalf("applyReplacement: %v", err)
	}
	if status != StatusUpdated {
		t.Fatalf("status = %s, want updated", status)
	}
	if got := fileSHA256(t, target); got != newSHA {
		t.Fatalf("target sha = %s, want %s", got, newSHA)
	}
}

func mustSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func TestApplyReplacementUnixUpdatesTarget(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly", 0o755)
	oldSHA := fileSHA256(t, target)

	candidate := filepath.Join(dir, candidateBaseName)
	newContent := []byte("#!/bin/sh\necho newer-binary\n")
	if err := os.WriteFile(candidate, newContent, 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA := fileSHA256(t, candidate)

	status, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: newSHA,
		OldSHA:       oldSHA,
		OldSize:      int64(len([]byte("old-binary"))),
	})
	if err != nil {
		t.Fatalf("applyReplacement: %v", err)
	}
	if status != StatusUpdated {
		t.Fatalf("status = %s, want updated", status)
	}
	if got := fileSHA256(t, target); got != newSHA {
		t.Fatalf("target sha = %s, want %s", got, newSHA)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatal("candidate should be consumed by rename")
	}
}

func TestApplyReplacementPreservesTargetBeforeRename(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly", 0o755)
	before := fileSHA256(t, target)

	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: strings.Repeat("a", 64),
		OldSHA:       before,
		OldSize:      mustSize(t, target),
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got := fileSHA256(t, target); got != before {
		t.Fatal("target mutated before rename")
	}
}

func TestApplyReplacementReportsPermissionRemediation(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly", 0o755)
	oldSHA := fileSHA256(t, target)

	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA := fileSHA256(t, candidate)

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := applyReplacement(replacementInput{
		Target:       target,
		Candidate:    candidate,
		CandidateSHA: newSHA,
		OldSHA:       oldSHA,
		OldSize:      mustSize(t, target),
	})
	if err == nil || !strings.Contains(err.Error(), "user-writable path") {
		t.Fatalf("err = %v", err)
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target mutated on permission failure")
	}
}

func TestRunRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	real := writeFakeBinary(t, dir, "dolly-real", 0o755)
	link := filepath.Join(dir, "dolly")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	assetName, err := AssetName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho newer\n")
	archive := buildTarGz(t, "dolly", content)
	checksums := []byte(checksumLine(assetName, archive))
	realSHA := fileSHA256(t, real)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       link,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v", err)
	}
	if result == nil || result.Status != StatusFailed {
		t.Fatalf("result = %+v", result)
	}
	if got := fileSHA256(t, real); got != realSHA {
		t.Fatal("real target mutated")
	}
}

func TestApplyReplacementRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	real := writeFakeBinary(t, dir, "dolly-real", 0o755)
	link := filepath.Join(dir, "dolly")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := applyReplacement(replacementInput{
		Target:       link,
		Candidate:    candidate,
		CandidateSHA: fileSHA256(t, candidate),
		OldSHA:       fileSHA256(t, real),
		OldSize:      10,
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunUpdatedUnix(t *testing.T) {
	assetName, err := AssetName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho updated-run\n")
	archive := buildTarGz(t, "dolly", content)
	checksums := []byte(checksumLine(assetName, archive))

	target := writeFakeBinary(t, t.TempDir(), "dolly", 0o755)
	before := fileSHA256(t, target)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusUpdated {
		t.Fatalf("status = %s, want updated", result.Status)
	}
	if after := fileSHA256(t, target); after == before {
		t.Fatal("target not updated")
	}
}
