//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

func TestDotenvPermissionAdvisory(t *testing.T) {
	dir := t.TempDir()
	broad := filepath.Join(dir, "broad.env")
	safe := filepath.Join(dir, "safe.env")
	if err := os.WriteFile(broad, []byte("DB_HOST=h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(safe, []byte("DB_HOST=h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path string
		wantBroad  bool
		checkMeta  bool
	}{
		{"broad0644", broad, true, true},
		{"safe0600", safe, false, false},
		{"missing", filepath.Join(dir, "missing.env"), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var before dotenvSnap
			var beforeUID, beforeGID int
			if tt.checkMeta {
				before = snapshotDotenv(t, tt.path, false)
				info, err := os.Stat(tt.path)
				if err != nil {
					t.Fatal(err)
				}
				st := info.Sys().(*syscall.Stat_t)
				beforeUID, beforeGID = int(st.Uid), int(st.Gid)
			}
			got, err := dotenvPermissionAdvisory(tt.path)
			if err != nil || got != tt.wantBroad {
				t.Fatalf("broad=%v err=%v want=%v", got, err, tt.wantBroad)
			}
			if tt.checkMeta {
				assertDotenvUnchanged(t, tt.path, before)
				after, err := os.Stat(tt.path)
				if err != nil {
					t.Fatal(err)
				}
				st := after.Sys().(*syscall.Stat_t)
				if int(st.Uid) != beforeUID || int(st.Gid) != beforeGID {
					t.Fatal("owner changed")
				}
			}
		})
	}
	t.Run("symlink_target_broad", func(t *testing.T) {
		target := filepath.Join(dir, "target.env")
		link := filepath.Join(dir, "link.env")
		if err := os.WriteFile(target, []byte("DB_HOST=h\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		before := snapshotDotenv(t, target, false)
		beforeLink, _ := os.Lstat(link)
		broad, err := dotenvPermissionAdvisory(link)
		if err != nil || !broad {
			t.Fatalf("broad=%v err=%v", broad, err)
		}
		assertDotenvUnchanged(t, target, before)
		afterLink, _ := os.Lstat(link)
		if afterLink.Mode().Perm() != beforeLink.Mode().Perm() {
			t.Fatal("symlink mode changed")
		}
	})
	t.Run("strict_boundary", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "owned-secret")
		if err := os.WriteFile(path, []byte("secret\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		broad, err := dotenvPermissionAdvisory(path)
		if err != nil || !broad {
			t.Fatalf("advisory broad=%v err=%v", broad, err)
		}
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o644 {
			t.Fatal("advisory must not chmod")
		}
		_ = ensureOwnerOnly(path)
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatal("ensureOwnerOnly must tighten")
		}
	})
}
