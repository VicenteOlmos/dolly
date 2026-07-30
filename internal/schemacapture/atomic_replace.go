//go:build !windows

package schemacapture

import "os"

// atomicReplaceFile atomically replaces dst with src. On Unix, os.Rename already
// replaces the destination when it exists.
func atomicReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
