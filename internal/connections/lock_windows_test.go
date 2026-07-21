//go:build windows

package connections

import (
	"path/filepath"
	"testing"
)

func TestLockFileExcludesAndReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.lock")
	first, err := lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockFile(path); err == nil {
		t.Fatal("second holder acquired lock")
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	second, err := lockFile(path)
	if err != nil {
		t.Fatalf("reacquire after close: %v", err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
}
