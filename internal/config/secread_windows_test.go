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
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "db=secret\n"
	if string(got) != want {
		t.Fatalf("bytes changed to %q, want %q (no-op)", got, want)
	}
}

func TestEnsureOwnerOnlyWindowsNilOnMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent")
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("expected nil on Windows for missing file, got %v", err)
	}
}
