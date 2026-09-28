package testutil

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// SkipFileModeChecks reports whether Unix-style permission bits are not enforced.
func SkipFileModeChecks() bool {
	return os.PathSeparator == '\\'
}

// AssertFilePerm fails when got perm differs from want on Unix-like systems.
func AssertFilePerm(t *testing.T, got, want os.FileMode, msg string) {
	t.Helper()
	if SkipFileModeChecks() {
		return
	}
	if got.Perm() != want.Perm() {
		t.Fatalf("%s = %o, want %o", msg, got.Perm(), want.Perm())
	}
}

// AssertElapsedPositive requires elapsed > 0 on Unix; on Windows coarse timers may report 0.
func AssertElapsedPositive(t *testing.T, elapsed time.Duration, msg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if elapsed < 0 {
			t.Fatalf("%s Elapsed = %v, want >= 0", msg, elapsed)
		}
		return
	}
	if elapsed <= 0 {
		t.Fatalf("%s Elapsed = %v, want > 0", msg, elapsed)
	}
}

// NormalizeNewlines folds CRLF/CR to LF for test comparisons.
func NormalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
