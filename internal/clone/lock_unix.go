//go:build !windows

package clone

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func init() {
	lockCacheAcquire = acquireCacheLock
	lockCacheRelease = releaseCacheLock
	lockCacheContention = isCacheLockContended
}

// acquireCacheLock acquires an exclusive nonblocking flock on f.
func acquireCacheLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

// releaseCacheLock releases the flock on f.
func releaseCacheLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

// isCacheLockContended reports whether err is a transient lock-contention
// error (EWOULDBLOCK or EAGAIN) that should trigger a retry.
func isCacheLockContended(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}
