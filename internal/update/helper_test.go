package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperSwapAndCleanup(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(dir, candidateBaseName)
	newContent := []byte("new-binary")
	if err := os.WriteFile(candidate, newContent, 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	capability := "aa" + repeatChar('b', 62)
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
	oldCleanupWait := cleanupWaitParentPID
	startDetachedProcess = func(path string, argv []string) error {
		return RunCleanup(manifestFile, capability)
	}
	cleanupWaitParentPID = func() int { return 9999999 }
	t.Cleanup(func() {
		startDetachedProcess = oldStart
		cleanupWaitParentPID = oldCleanupWait
	})

	if err := RunHelper(manifestFile, capability); err != nil {
		t.Fatalf("RunHelper: %v", err)
	}
	if got := fileSHA256(t, target); got != newSHA {
		t.Fatalf("target sha = %s, want %s", got, newSHA)
	}
	if _, err := os.Stat(manifestFile); !os.IsNotExist(err) {
		t.Fatal("manifest should be removed")
	}
	report := filepath.Join(dir, reportBaseName)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "updated") {
		t.Fatalf("report = %s", string(data))
	}
}

func TestHelperRollbackOnDigestMismatch(t *testing.T) {
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
	capability := repeatChar('c', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     9999999,
		Target:        target,
		Candidate:     candidate,
		Backup:        backupPath(dir),
		Helper:        helperPath(dir),
		OldSHA256:     oldSHA,
		NewSHA256:     repeatChar('d', 64),
		OldSize:       oldSize,
		NewSize:       10,
		RemoteVersion: "v0.3.2",
	}
	manifestFile := manifestPath(dir)
	if err := writeManifest(manifestFile, manifest); err != nil {
		t.Fatal(err)
	}

	if err := RunHelper(manifestFile, capability); err == nil {
		t.Fatal("expected helper failure")
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target should be restored/unchanged")
	}
}

func TestHelperInvalidCapability(t *testing.T) {
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
	capability := repeatChar('c', 64)
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

	if err := RunHelper(manifestFile, "bad-capability"); err == nil {
		t.Fatal("expected invalid capability failure")
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target should remain unchanged")
	}
}

func TestRunCleanupPublishesReport(t *testing.T) {
	dir := t.TempDir()
	backup := backupPath(dir)
	if err := os.WriteFile(backup, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSHA, oldSize, err := fileDigest(backup)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "dolly.exe")
	if err := os.WriteFile(target, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}

	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath(dir), []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('e', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     1,
		Target:        target,
		Candidate:     candidate,
		Backup:        backup,
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

	oldCleanupWait := cleanupWaitParentPID
	cleanupWaitParentPID = func() int { return 9999999 }
	t.Cleanup(func() { cleanupWaitParentPID = oldCleanupWait })

	if err := RunCleanup(manifestFile, capability); err != nil {
		t.Fatalf("RunCleanup: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup should be removed")
	}
	if _, err := os.Stat(manifestFile); !os.IsNotExist(err) {
		t.Fatal("manifest should be removed")
	}
	report := filepath.Join(dir, reportBaseName)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(data), "updated") {
		t.Fatalf("report = %s", string(data))
	}
}

func TestHelperRollbackOnSwapFailure(t *testing.T) {
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

	capability := repeatChar('f', 64)
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
		t.Fatal("target should be restored after swap failure")
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

func TestHelperFailurePreservesReportError(t *testing.T) {
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

	if err := os.WriteFile(reportTempPath(dir), []byte("blocked"), 0o600); err != nil {
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

	err = RunHelper(manifestFile, capability)
	if err == nil {
		t.Fatal("expected combined failure")
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Fatalf("swap error missing from %v", err)
	}
	if !strings.Contains(err.Error(), "write update report temp") {
		t.Fatalf("report error missing from %v", err)
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target should be restored after swap failure")
	}
	if _, readErr := os.ReadFile(reportPath(dir)); readErr == nil {
		t.Fatal("report should not be published when temp write fails")
	}
}

func TestRunCleanupFailsWhenReportPublicationFails(t *testing.T) {
	dir := t.TempDir()
	backup := backupPath(dir)
	if err := os.WriteFile(backup, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSHA, oldSize, err := fileDigest(backup)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(dir, "dolly.exe")
	if err := os.WriteFile(target, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('b', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     1,
		Target:        target,
		Candidate:     filepath.Join(dir, candidateBaseName),
		Backup:        backup,
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

	if err := os.WriteFile(reportTempPath(dir), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldCleanupWait := cleanupWaitParentPID
	cleanupWaitParentPID = func() int { return 9999999 }
	t.Cleanup(func() { cleanupWaitParentPID = oldCleanupWait })

	err = RunCleanup(manifestFile, capability)
	if err == nil || !strings.Contains(err.Error(), "write update report temp") {
		t.Fatalf("err = %v", err)
	}
	if _, readErr := os.ReadFile(reportPath(dir)); readErr == nil {
		t.Fatal("report should not be published when temp write fails")
	}
}

type helperFixture struct {
	dir          string
	target       string
	candidate    string
	manifest     updateManifest
	manifestFile string
	capability   string
	oldSHA       string
	newSHA       string
}

func newHelperFixture(t *testing.T) helperFixture {
	t.Helper()
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
	return helperFixture{
		dir:          dir,
		target:       target,
		candidate:    candidate,
		manifest:     manifest,
		manifestFile: manifestFile,
		capability:   capability,
		oldSHA:       oldSHA,
		newSHA:       newSHA,
	}
}

func TestHelperRejectsPostWaitTargetMutation(t *testing.T) {
	fx := newHelperFixture(t)
	oldWait := waitForPIDExit
	waitForPIDExit = func(pid int, timeout time.Duration) error {
		if err := os.WriteFile(fx.target, []byte("mutated-target"), 0o755); err != nil {
			return err
		}
		return nil
	}
	t.Cleanup(func() { waitForPIDExit = oldWait })

	err := RunHelper(fx.manifestFile, fx.capability)
	if err == nil {
		t.Fatal("expected post-wait target mutation failure")
	}
	if !strings.Contains(err.Error(), "target digest mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got := fileSHA256(t, fx.target); got == fx.newSHA {
		t.Fatal("target must not be swapped to candidate after mutation")
	}
}

func TestHelperRejectsPostWaitCandidateMutation(t *testing.T) {
	fx := newHelperFixture(t)
	oldWait := waitForPIDExit
	waitForPIDExit = func(pid int, timeout time.Duration) error {
		if err := os.WriteFile(fx.candidate, []byte("mutated-candidate"), 0o755); err != nil {
			return err
		}
		return nil
	}
	t.Cleanup(func() { waitForPIDExit = oldWait })

	err := RunHelper(fx.manifestFile, fx.capability)
	if err == nil {
		t.Fatal("expected post-wait candidate mutation failure")
	}
	if !strings.Contains(err.Error(), "candidate digest mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got := fileSHA256(t, fx.target); got != fx.oldSHA {
		t.Fatal("target should remain unchanged")
	}
}

func TestHelperRemovesStaleBackupBeforeSwap(t *testing.T) {
	fx := newHelperFixture(t)
	if err := os.WriteFile(fx.manifest.Backup, []byte("stale-backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldStart := startDetachedProcess
	oldCleanupWait := cleanupWaitParentPID
	startDetachedProcess = func(path string, argv []string) error {
		return RunCleanup(fx.manifestFile, fx.capability)
	}
	cleanupWaitParentPID = func() int { return 9999999 }
	t.Cleanup(func() {
		startDetachedProcess = oldStart
		cleanupWaitParentPID = oldCleanupWait
	})

	if err := RunHelper(fx.manifestFile, fx.capability); err != nil {
		t.Fatalf("RunHelper: %v", err)
	}
	if got := fileSHA256(t, fx.target); got != fx.newSHA {
		t.Fatalf("target sha = %s, want %s", got, fx.newSHA)
	}
}

func TestHelperRejectsStaleBackupOnFirstRenameFailure(t *testing.T) {
	fx := newHelperFixture(t)
	if err := os.WriteFile(fx.manifest.Backup, []byte("stale-backup"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == fx.target && newpath == fx.manifest.Backup {
			return os.ErrPermission
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	err := RunHelper(fx.manifestFile, fx.capability)
	if err == nil {
		t.Fatal("expected first rename failure")
	}
	if got := fileSHA256(t, fx.target); got != fx.oldSHA {
		t.Fatal("target must not be replaced by stale backup")
	}
	if _, statErr := os.Stat(fx.manifest.Backup); !os.IsNotExist(statErr) {
		t.Fatal("stale backup should be removed before rename attempt")
	}
}

func TestHelperSingleRollbackOnSwapFailure(t *testing.T) {
	fx := newHelperFixture(t)
	var rollbackCalls int
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == fx.manifest.Backup && newpath == fx.target {
			rollbackCalls++
		}
		if oldpath == fx.candidate && newpath == fx.target {
			return os.ErrPermission
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	if err := RunHelper(fx.manifestFile, fx.capability); err == nil {
		t.Fatal("expected swap failure")
	}
	if rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", rollbackCalls)
	}
	if got := fileSHA256(t, fx.target); got != fx.oldSHA {
		t.Fatal("target should be restored after swap failure")
	}
}

func TestHelperJoinsPrimaryAndRollbackErrors(t *testing.T) {
	fx := newHelperFixture(t)
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == fx.candidate && newpath == fx.target {
			return os.ErrPermission
		}
		if oldpath == fx.manifest.Backup && newpath == fx.target {
			return os.ErrExist
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	err := RunHelper(fx.manifestFile, fx.capability)
	if err == nil {
		t.Fatal("expected joined failure")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("primary error missing: %v", err)
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("rollback error missing: %v", err)
	}
}

func TestHelperVerifiesRestoredDigestAndSize(t *testing.T) {
	fx := newHelperFixture(t)
	oldSHA, oldSize, err := fileDigest(fx.target)
	if err != nil {
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

	if err := RunHelper(fx.manifestFile, fx.capability); err == nil {
		t.Fatal("expected swap failure")
	}
	gotSHA, gotSize, err := fileDigest(fx.target)
	if err != nil {
		t.Fatal(err)
	}
	if gotSHA != oldSHA || gotSize != oldSize {
		t.Fatalf("restored target digest/size = %s/%d, want %s/%d", gotSHA, gotSize, oldSHA, oldSize)
	}
}

func TestHelperPreservesArtifactsOnRollbackFailure(t *testing.T) {
	fx := newHelperFixture(t)
	if err := os.WriteFile(fx.manifest.Helper, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == fx.candidate && newpath == fx.target {
			return os.ErrPermission
		}
		if oldpath == fx.manifest.Backup && newpath == fx.target {
			return os.ErrExist
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	if err := RunHelper(fx.manifestFile, fx.capability); err == nil {
		t.Fatal("expected failure")
	}
	for _, path := range []string{fx.manifest.Backup, fx.manifest.Helper, fx.manifestFile} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("artifact %s should be preserved: %v", path, statErr)
		}
	}
}

func TestHelperFailsWhenRestoredDigestMismatch(t *testing.T) {
	fx := newHelperFixture(t)
	oldRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		if oldpath == fx.candidate && newpath == fx.target {
			return os.ErrPermission
		}
		if oldpath == fx.manifest.Backup && newpath == fx.target {
			if err := oldRename(oldpath, newpath); err != nil {
				return err
			}
			return os.WriteFile(fx.target, []byte("corrupt-restore"), 0o755)
		}
		return oldRename(oldpath, newpath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	err := RunHelper(fx.manifestFile, fx.capability)
	if err == nil {
		t.Fatal("expected restored digest mismatch failure")
	}
	if !strings.Contains(err.Error(), "restored target digest mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func repeatChar(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}
