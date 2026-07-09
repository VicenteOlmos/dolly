//go:build windows

package dumphistory

import (
	"fmt"
	"os"
	"path/filepath"
)

type histLock struct {
	f    *os.File
	path string
}

func lockHistFile(path string) (*histLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	// ponytail: atomic lockfile create as poor-man's flock on Windows.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("lock file already held: %s", path)
		}
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	return &histLock{f: f, path: path}, nil
}

func (l *histLock) close() error {
	closeErr := l.f.Close()
	os.Remove(l.path) // ponytail: best-effort cleanup
	return closeErr
}
