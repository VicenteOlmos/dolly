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

func TestDotenvPermissionAdvisoryWindows(t *testing.T) {
	dir := t.TempDir()
	broad := filepath.Join(dir, ".env")
	if err := os.WriteFile(broad, []byte("DB_HOST=h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{broad, filepath.Join(dir, "missing.env")} {
		got, err := dotenvPermissionAdvisory(path)
		if err != nil || got {
			t.Fatalf("%s: broad=%v err=%v", path, got, err)
		}
	}
	path := filepath.Join(t.TempDir(), "owned-secret")
	const content = "secret\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("strict bytes=%q err=%v", got, err)
	}
}
