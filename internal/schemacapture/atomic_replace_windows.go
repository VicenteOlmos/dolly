//go:build windows

package schemacapture

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// atomicReplaceFile atomically replaces dst with src. On Windows, MoveFileEx with
// MOVEFILE_REPLACE_EXISTING and MOVEFILE_WRITE_THROUGH ensures the destination
// is replaced even when it already exists and writes are flushed to disk.
func atomicReplaceFile(src, dst string) error {
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
