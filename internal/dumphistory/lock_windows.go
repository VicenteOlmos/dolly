//go:build windows

package dumphistory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

var ErrLockTimeout = errors.New("dump history lock acquisition timed out")

const lockTimeout = 2 * time.Second

var (
	lockHistFileEx = windows.LockFileEx
	histLockNow    = time.Now
	histLockSleep  = time.Sleep
)

type histLock struct {
	f *os.File
}

func lockHistFile(path string) (*histLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	deadline := histLockNow().Add(lockTimeout)
	for delay := time.Millisecond; ; delay = min(delay*2, 50*time.Millisecond) {
		var overlapped windows.Overlapped
		err := lockHistFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
		if err == nil {
			return &histLock{f: f}, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = f.Close()
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		if histLockNow().Add(delay).After(deadline) {
			_ = f.Close()
			return nil, ErrLockTimeout
		}
		histLockSleep(delay)
	}
}

func (l *histLock) close() error {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &overlapped)
	return l.f.Close()
}
