package clone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// errCacheLockTimeout is returned when cache lock acquisition exceeds the deadline.
var errCacheLockTimeout = errors.New("cache lock acquisition timed out")

const (
	cacheLockTimeout      = 2 * time.Second
	cacheLockBackoffStart = 1 * time.Millisecond
	cacheLockBackoffMax   = 50 * time.Millisecond
)

// Seam variables — injectable for testing.
var (
	lockCacheAcquire    func(*os.File) error // per-platform acquire
	lockCacheRelease    func(*os.File) error // per-platform unlock
	lockCacheClose      = func(f *os.File) error { return f.Close() }
	lockCacheContention func(error) bool // per-platform contention predicate
	lockNow             = time.Now
	lockSleep           = time.Sleep
)

// cacheLock holds the acquired lock file descriptor.
type cacheLock struct {
	f *os.File
}

// lockCacheFile acquires an exclusive lock on the cache sidecar at path.
// It creates parent directories with 0o700 and the lock file with 0o600.
// Acquisition retries only contention errors with deterministic backoff
// (1 ms start, ×2 cap 50 ms) until the deadline (2 s). Non-contention
// errors fail immediately. Returns errCacheLockTimeout on deadline.
func lockCacheFile(path string) (*cacheLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	deadline := lockNow().Add(cacheLockTimeout)
	for delay := cacheLockBackoffStart; ; delay = min(delay*2, cacheLockBackoffMax) {
		err := lockCacheAcquire(f)
		if err == nil {
			return &cacheLock{f: f}, nil
		}
		if !lockCacheContention(err) {
			_ = f.Close()
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		if lockNow().Add(delay).After(deadline) {
			_ = f.Close()
			return nil, errCacheLockTimeout
		}
		lockSleep(delay)
	}
}

// close releases the lock (unlock before close). Unlock and close are each
// attempted; close is attempted even if unlock fails. Returns the first
// failing cause (unlock error takes priority over close error).
func (l *cacheLock) close() error {
	unlockErr := lockCacheRelease(l.f)
	closeErr := lockCacheClose(l.f)
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
