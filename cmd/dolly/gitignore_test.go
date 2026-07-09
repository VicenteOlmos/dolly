package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestGitignoreAudit verifies that sensitive local files are ignored by git
// and cannot be accidentally committed. Skip in short mode; requires git binary.
func TestGitignoreAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping gitignore audit in short mode")
	}

	// Pre-flight: ensure git is available and we are in a repo.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	wantIgnored := []string{
		".env",
		".env.local",
		".env.production",
		".dolly/",
		".dolly.connections.yaml",
		"test.connections.yaml",
		"dolly_dump/",
		"dumps/",
	}

	for _, path := range wantIgnored {
		t.Run(path, func(t *testing.T) {
			out, err := exec.Command("git", "check-ignore", "-q", path).CombinedOutput()
			if err != nil {
				// check-ignore exits 1 when path is NOT ignored.
				t.Errorf("%q is NOT ignored by git: %v\n%s", path, err, string(out))
			}
		})
	}

	// .env.example and .env.sample should NOT be ignored (allowlisted for docs).
	wantTracked := []string{
		".env.example",
		".env.sample",
	}

	for _, path := range wantTracked {
		t.Run("tracked/"+path, func(t *testing.T) {
			out, err := exec.Command("git", "check-ignore", "-q", path).CombinedOutput()
			// check-ignore exits 0 when path IS ignored; we want it NOT ignored (exit 1).
			if err == nil {
				t.Errorf("%q SHOULD NOT be ignored but it IS: %s", path, string(out))
			}
		})
	}
}

// TestGitignoreSecrets confirms no .env or connections files are tracked.
func TestGitignoreSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping secret file tracking check in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// git ls-files returns tracked files. These should be empty.
	checks := []string{".env", ".dolly.connections.yaml", "dolly_dump/", "dumps/"}
	for _, path := range checks {
		out, err := exec.Command("git", "ls-files", "--", path).Output()
		if err != nil {
			t.Fatalf("git ls-files %s: %v", path, err)
		}
		tracked := strings.TrimSpace(string(out))
		if tracked != "" {
			t.Errorf("%q is TRACKED in git (should be ignored):\n%s", path, tracked)
		}
	}
}
