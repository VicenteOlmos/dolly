package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForPIDExitAlreadyGone(t *testing.T) {
	if err := defaultWaitForPIDExit(9999999, time.Second); err != nil {
		t.Fatalf("waitForPIDExit: %v", err)
	}
}

func TestWaitForPIDExitBlocksUntilChildExits(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess timing")
	}
	cmd := exec.Command("sleep", "0.2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	start := time.Now()
	if err := defaultWaitForPIDExit(cmd.Process.Pid, 5*time.Second); err != nil {
		t.Fatalf("waitForPIDExit: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("returned too early after %v", elapsed)
	}
}

func TestWaitForPIDExitTimesOut(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	err := defaultWaitForPIDExit(cmd.Process.Pid, 100*time.Millisecond)
	if err == nil || err.Error() != "timed out waiting for pid "+strconv.Itoa(cmd.Process.Pid) {
		t.Fatalf("err = %v", err)
	}
}

func TestHelperAbortsWhenParentWaitFails(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	beforeSHA := fileSHA256(t, target)
	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('e', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     4242,
		Target:        target,
		Candidate:     candidate,
		Backup:        backupPath(dir),
		Helper:        helperPath(dir),
		OldSHA256:     oldSHA,
		NewSHA256:     newSHA,
		OldSize:       oldSize,
		NewSize:       newSize,
		RemoteVersion: "v0.3.2",
	}
	manifestFile := manifestPath(dir)
	if err := writeManifest(manifestFile, manifest); err != nil {
		t.Fatal(err)
	}

	oldWait := waitForPIDExit
	waitForPIDExit = func(pid int, timeout time.Duration) error {
		if pid != manifest.ParentPID {
			t.Fatalf("wait pid = %d, want %d", pid, manifest.ParentPID)
		}
		return fmt.Errorf("open pid %d: access denied", pid)
	}
	t.Cleanup(func() { waitForPIDExit = oldWait })

	if err := RunHelper(manifestFile, capability); err == nil {
		t.Fatal("expected parent wait failure")
	}
	if got := fileSHA256(t, target); got != beforeSHA {
		t.Fatal("target mutated when parent wait failed")
	}
	if _, err := os.Stat(backupPath(dir)); !os.IsNotExist(err) {
		t.Fatal("backup created when parent wait failed")
	}
}

func TestHelperWaitsForParentBeforeSwap(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('a', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     os.Getpid(),
		Target:        target,
		Candidate:     candidate,
		Backup:        backupPath(dir),
		Helper:        helperPath(dir),
		OldSHA256:     oldSHA,
		NewSHA256:     newSHA,
		OldSize:       oldSize,
		NewSize:       newSize,
		RemoteVersion: "v0.3.2",
	}
	manifestFile := manifestPath(dir)
	if err := writeManifest(manifestFile, manifest); err != nil {
		t.Fatal(err)
	}

	var waited atomic.Bool
	var swapped atomic.Bool
	oldWait := waitForPIDExit
	waitForPIDExit = func(pid int, timeout time.Duration) error {
		if pid != os.Getpid() {
			t.Fatalf("wait pid = %d, want %d", pid, os.Getpid())
		}
		waited.Store(true)
		return nil
	}
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if !waited.Load() {
			t.Fatal("swap attempted before parent wait")
		}
		swapped.Store(true)
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() {
		waitForPIDExit = oldWait
		renameFile = oldRename
	})

	oldStart := startDetachedProcess
	startDetachedProcess = func(path string, argv []string) error { return nil }
	t.Cleanup(func() { startDetachedProcess = oldStart })

	if err := RunHelper(manifestFile, capability); err != nil {
		t.Fatalf("RunHelper: %v", err)
	}
	if !waited.Load() || !swapped.Load() {
		t.Fatal("expected parent wait before swap")
	}
}

func TestHelperRollbackAfterBackupRename(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('b', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     9999999,
		Target:        target,
		Candidate:     candidate,
		Backup:        backupPath(dir),
		Helper:        helperPath(dir),
		OldSHA256:     oldSHA,
		NewSHA256:     newSHA,
		OldSize:       oldSize,
		NewSize:       newSize,
		RemoteVersion: "v0.3.2",
	}
	manifestFile := manifestPath(dir)
	if err := writeManifest(manifestFile, manifest); err != nil {
		t.Fatal(err)
	}

	var renameCalls int
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		renameCalls++
		if renameCalls == 2 {
			return os.ErrPermission
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	if err := RunHelper(manifestFile, capability); err == nil {
		t.Fatal("expected swap failure")
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target should be restored from backup")
	}
	report := filepath.Join(dir, reportBaseName)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "failed") {
		t.Fatalf("report = %s", string(data))
	}
}

func TestHelperUpdatedTargetRemainsUsable(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, candidateBaseName)
	newContent := []byte("#!/bin/sh\necho helper-usable\n")
	if err := os.WriteFile(candidate, newContent, 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('d', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     9999999,
		Target:        target,
		Candidate:     candidate,
		Backup:        backupPath(dir),
		Helper:        helperPath(dir),
		OldSHA256:     oldSHA,
		NewSHA256:     newSHA,
		OldSize:       oldSize,
		NewSize:       newSize,
		RemoteVersion: "v0.3.2",
	}
	manifestFile := manifestPath(dir)
	if err := writeManifest(manifestFile, manifest); err != nil {
		t.Fatal(err)
	}

	oldStart := startDetachedProcess
	startDetachedProcess = func(path string, argv []string) error {
		return RunCleanup(manifestFile, capability)
	}
	oldCleanupWait := cleanupWaitParentPID
	cleanupWaitParentPID = func() int { return 9999999 }
	t.Cleanup(func() {
		startDetachedProcess = oldStart
		cleanupWaitParentPID = oldCleanupWait
	})

	if err := RunHelper(manifestFile, capability); err != nil {
		t.Fatalf("RunHelper: %v", err)
	}
	out, err := exec.Command(target).Output()
	if err != nil {
		t.Fatalf("execute updated target: %v", err)
	}
	if !strings.Contains(string(out), "helper-usable") {
		t.Fatalf("output = %q", string(out))
	}
}

func TestReportDurabilityAtomicPublish(t *testing.T) {
	dir := t.TempDir()
	if err := writePendingReport(dir, pendingReport{
		Status:        StatusUpdated,
		RemoteVersion: "v0.3.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reportTempPath(dir)); !os.IsNotExist(err) {
		t.Fatal("temp report should not remain after publish")
	}
	data, err := os.ReadFile(reportPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "updated") {
		t.Fatalf("report = %s", string(data))
	}
}
