package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManifestRejectsInvalidParentPID(t *testing.T) {
	capability := strings.Repeat("a", 64)
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

func TestValidateManifestDigestsRejectsMismatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dolly")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
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

	manifest := updateManifest{
		Target:    target,
		Candidate: candidate,
		OldSHA256: oldSHA,
		NewSHA256: strings.Repeat("d", 64),
		OldSize:   oldSize,
		NewSize:   newSize,
	}
	if err := validateManifestDigests(manifest); err == nil || !strings.Contains(err.Error(), "candidate digest mismatch") {
		t.Fatalf("err = %v", err)
	}

	manifest.NewSHA256 = newSHA
	manifest.OldSHA256 = strings.Repeat("e", 64)
	if err := validateManifestDigests(manifest); err == nil || !strings.Contains(err.Error(), "target digest mismatch") {
		t.Fatalf("err = %v", err)
	}
}
