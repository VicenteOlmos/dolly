//go:build !windows

package clone

import (
	"fmt"
	"os"
)

// atomicReplace atomically replaces dst with src. On Unix, os.Rename already
// replaces the destination when it exists.
func atomicReplace(src, dst string) error {
	return os.Rename(src, dst)
}

// ensureCacheOwnerOnly tightens path to owner-only (0600) before reading or
// writing cache bytes. A missing file is not an error.
func ensureCacheOwnerOnly(path string) error {
	return ensureCacheOwnerOnlyImpl(path)
}

var ensureCacheOwnerOnlyImpl = func(path string) error {
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
