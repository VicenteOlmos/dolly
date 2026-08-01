//go:build windows

package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyReplacementWindowsDeferred(t *testing.T) {
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
	newSHA, _, err := fileDigest(candidate)
	if err != nil {
		t.Fatal(err)
	}

	oldStart := startDetachedProcess
	startDetachedProcess = func(path string, argv []string) error {
		if !strings.Contains(path, helperBaseName) {
			t.Fatalf("helper path = %q", path)
		}
		if len(argv) < 3 || argv[1] != "__update-helper" {
			t.Fatalf("argv = %v", argv)
		}
		return nil
	}
	t.Cleanup(func() { startDetachedProcess = oldStart })

	status, err := applyReplacement(replacementInput{
		Target:        target,
		Candidate:     candidate,
		CandidateSHA:  newSHA,
		OldSHA:        oldSHA,
		OldSize:       oldSize,
		RemoteVersion: "v0.3.2",
	})
	if err != nil {
		t.Fatalf("applyReplacement: %v", err)
	}
	if status != StatusDeferred {
		t.Fatalf("status = %s, want deferred", status)
	}
	if _, err := os.Stat(helperPath(dir)); err != nil {
		t.Fatalf("helper missing: %v", err)
	}
	if _, err := os.Stat(manifestPath(dir)); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target mutated before helper runs")
	}
}
