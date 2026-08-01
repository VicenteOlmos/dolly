package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeferredLayoutForWindows(t *testing.T) {
	dir := "/tmp/dolly-update"
	layout := deferredLayoutFor(dir, "windows")
	if !strings.HasSuffix(layout.Target, "dolly.exe") {
		t.Fatalf("target = %q", layout.Target)
	}
	if !strings.HasSuffix(layout.Helper, ".dolly-update-helper.exe") {
		t.Fatalf("helper = %q", layout.Helper)
	}
	if layout.Manifest != filepath.Join(dir, manifestBaseName) {
		t.Fatalf("manifest = %q", layout.Manifest)
	}
}

func TestHelperArgvContract(t *testing.T) {
	argv := helperArgv(`C:\bin\.dolly-update-helper.exe`, `C:\bin\.dolly-update-manifest.json`, repeatChar('a', 64))
	if len(argv) != 4 || argv[1] != "__update-helper" {
		t.Fatalf("argv = %v", argv)
	}
	cleanup := cleanupArgv(`C:\bin\dolly.exe`, argv[2], argv[3])
	if len(cleanup) != 4 || cleanup[1] != "__update-cleanup" {
		t.Fatalf("cleanup argv = %v", cleanup)
	}
}

func TestPrepareDeferredReplacementLinux(t *testing.T) {
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
	newSHA := fileSHA256(t, candidate)

	var launched []string
	status, err := prepareDeferredReplacement(replacementInput{
		Target:        target,
		Candidate:     candidate,
		CandidateSHA:  newSHA,
		OldSHA:        oldSHA,
		OldSize:       oldSize,
		RemoteVersion: "v0.3.2",
	}, deferredPrepOptions{
		GOOS:      "windows",
		ParentPID: 4242,
		CopyExec: func(dst string) error {
			return os.WriteFile(dst, []byte("helper-copy"), 0o755)
		},
		Launch: func(path string, argv []string) error {
			launched = append([]string(nil), argv...)
			if !strings.Contains(path, helperBaseName) {
				t.Fatalf("helper path = %q", path)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("prepareDeferredReplacement: %v", err)
	}
	if status != StatusDeferred {
		t.Fatalf("status = %s", status)
	}
	if len(launched) != 4 || launched[1] != "__update-helper" {
		t.Fatalf("launched = %v", launched)
	}
	if got := fileSHA256(t, target); got != oldSHA {
		t.Fatal("target mutated during deferred prep")
	}
	manifestFile := filepath.Join(dir, manifestBaseName)
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"parent_pid":4242`) {
		t.Fatalf("manifest = %s", string(data))
	}
}

func TestPrepareDeferredReplacementRejectsBadCandidatePath(t *testing.T) {
	dir := t.TempDir()
	target := writeFakeBinary(t, dir, "dolly.exe", 0o755)
	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		t.Fatal(err)
	}
	otherDir := t.TempDir()
	candidate := filepath.Join(otherDir, candidateBaseName)
	if err := os.WriteFile(candidate, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	newSHA := fileSHA256(t, candidate)

	_, err = prepareDeferredReplacement(replacementInput{
		Target:        target,
		Candidate:     candidate,
		CandidateSHA:  newSHA,
		OldSHA:        oldSHA,
		OldSize:       oldSize,
		RemoteVersion: "v0.3.2",
	}, deferredPrepOptions{
		GOOS: "windows",
		CopyExec: func(dst string) error {
			return os.WriteFile(dst, []byte("helper"), 0o755)
		},
		Launch: func(path string, argv []string) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("err = %v", err)
	}
}
