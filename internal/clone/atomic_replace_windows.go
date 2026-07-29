//go:build windows

package clone

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// atomicReplace atomically replaces dst with src. On Windows, MoveFileEx with
// MOVEFILE_REPLACE_EXISTING and MOVEFILE_WRITE_THROUGH ensures the destination
// is replaced even when it already exists and writes are flushed to disk.
func atomicReplace(src, dst string) error {
	srcPtr, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return fmt.Errorf("atomic replace src: %w", err)
	}
	dstPtr, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return fmt.Errorf("atomic replace dst: %w", err)
	}
	return windows.MoveFileEx(srcPtr, dstPtr, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// ensureCacheOwnerOnly is a no-op on Windows where Unix owner/group/other mode
// semantics do not apply.
func ensureCacheOwnerOnly(path string) error {
	return ensureCacheOwnerOnlyImpl(path)
}

var ensureCacheOwnerOnlyImpl = func(string) error { return nil }
