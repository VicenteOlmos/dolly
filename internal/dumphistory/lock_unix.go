//go:build !windows

package dumphistory

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
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
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return &histLock{f: f}, nil
}

func (l *histLock) close() error {
	return l.f.Close() // flock released on close
}
