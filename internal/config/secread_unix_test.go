//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureOwnerOnlyTightensBroadMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("db=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureOwnerOnlyIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("db=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("second call: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after second call = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureOwnerOnlyENOENTReturnsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent")
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("expected nil for ENOENT, got %v", err)
	}
}

func TestEnsureOwnerOnlyAlready0600Noop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("db=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureOwnerOnly(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureOwnerOnlyFailClosedPreventsRead(t *testing.T) {
	content := `{"clone":{"strategy":"template"}}`
	path := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := ensureOwnerOnlyImpl
	ensureOwnerOnlyImpl = func(string) error {
		return fmt.Errorf("injected tighten failure")
	}
	t.Cleanup(func() { ensureOwnerOnlyImpl = orig })

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error from injected tighten failure")
	}
	if !strings.Contains(err.Error(), "injected tighten failure") {
		t.Fatalf("expected tighten error, got %v", err)
	}
	if strings.Contains(err.Error(), "parse config") {
		t.Fatalf("parse error should not win over tighten: %v", err)
	}
}
