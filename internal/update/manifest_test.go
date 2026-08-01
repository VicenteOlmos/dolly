package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateManifestRejectsInvalidParentPID(t *testing.T) {
	capability := repeatChar('a', 64)
	manifest := updateManifest{
		Capability: capability,
		ParentPID:  0,
		Target:     filepath.Join(t.TempDir(), "dolly.exe"),
		Candidate:  filepath.Join(t.TempDir(), candidateBaseName),
		Backup:     backupPath(t.TempDir()),
		Helper:     helperPath(t.TempDir()),
	}
	if err := validateManifest(manifest, capability); err == nil {
		t.Fatal("expected invalid parent pid rejection")
	}
}

func TestHelperRejectsInvalidParentPIDWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	beforeSHA := fileSHA256(t, target)
	candidate := filepath.Join(dir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	newSHA, newSize, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	capability := repeatChar('b', 64)
	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     -1,
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

	var waitCalled bool
	oldWait := waitForPIDExit
	waitForPIDExit = func(pid int, timeout time.Duration) error {
		waitCalled = true
		return nil
	}
	t.Cleanup(func() { waitForPIDExit = oldWait })

	if err := RunHelper(manifestFile, capability); err == nil {
		t.Fatal("expected invalid parent pid failure")
	}
	if waitCalled {
		t.Fatal("waitForPIDExit should not run for invalid parent pid")
	}
	if got := fileSHA256(t, target); got != beforeSHA {
		t.Fatal("target mutated on invalid parent pid")
	}
	if _, err := os.Stat(backupPath(dir)); !os.IsNotExist(err) {
		t.Fatal("backup created on invalid parent pid")
	}
	if _, err := os.Stat(reportPath(dir)); !os.IsNotExist(err) {
		t.Fatal("report published on invalid parent pid")
	}
}
