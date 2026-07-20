//go:build windows

package dumphistory

import (
	"path/filepath"
	"testing"
)

func TestLockHistFileExcludesAndReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.lock")
	first, err := lockHistFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockHistFile(path); err == nil {
		t.Fatal("second holder acquired lock")
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	second, err := lockHistFile(path)
	if err != nil {
		t.Fatalf("reacquire after close: %v", err)
	}
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
}
