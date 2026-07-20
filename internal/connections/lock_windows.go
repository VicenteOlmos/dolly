//go:build windows

package connections

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type connLock struct {
	f *os.File
}

func lockFile(path string) (*connLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return &connLock{f: f}, nil
}

func (l *connLock) close() error {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &overlapped)
	return l.f.Close()
}
