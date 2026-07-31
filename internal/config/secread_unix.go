//go:build !windows

package config

import (
	"fmt"
	"os"
)

// dotenvPermissionAdvisory reports whether path has group or other permission
// bits set. It performs os.Stat only and never mutates the file.
// A missing file is not an error and is not considered broad.
func dotenvPermissionAdvisory(path string) (broad bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	return info.Mode().Perm()&0o077 != 0, nil
}

// ensureOwnerOnly tightens path to owner-only (0600) before reading secret
// bytes. A missing file is not an error (preserves default config and shell
// env fallback).
func ensureOwnerOnly(path string) error {
	return ensureOwnerOnlyImpl(path)
}

var ensureOwnerOnlyImpl = func(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("tighten permissions %s (mode %o): %w", path, perm, err)
		}
	}
	return nil
}
