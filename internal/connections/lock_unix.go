//go:build !windows

package connections

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
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
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	return &connLock{f: f}, nil
}

func (l *connLock) close() error {
	return l.f.Close() // flock released on close
}
