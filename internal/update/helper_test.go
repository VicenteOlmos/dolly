package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	capability := strings.Repeat("e", 64)
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

	capability := strings.Repeat("b", 64)
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
