package update

import (
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
