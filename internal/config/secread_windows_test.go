//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureOwnerOnlyWindowsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("db=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("expected no error on Windows no-op, got %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode changed to %o, want 0644 (no-op)", info.Mode().Perm())
	}
}

func TestEnsureOwnerOnlyWindowsNilOnMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent")
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("expected nil on Windows for missing file, got %v", err)
	}
}
