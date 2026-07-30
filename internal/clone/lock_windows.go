//go:build windows

package clone

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	lockCacheAcquire = acquireCacheLock
	lockCacheRelease = releaseCacheLock
	lockCacheContention = isCacheLockContended
}

// acquireCacheLock acquires an exclusive fail-immediately lock on f
// via LockFileEx.
func acquireCacheLock(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	)
}

// releaseCacheLock releases the LockFileEx lock on f.
func releaseCacheLock(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
}

// isCacheLockContended reports whether err is ERROR_LOCK_VIOLATION,
// a transient lock-contention error that should trigger a retry.
func isCacheLockContended(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
