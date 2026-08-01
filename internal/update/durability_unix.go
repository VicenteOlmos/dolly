//go:build !windows

package update

import (
	"os"

	"golang.org/x/sys/unix"
)

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Fsync(int(f.Fd()))
}

func syncOpenFile(f *os.File) error {
	return unix.Fsync(int(f.Fd()))
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return unix.Fsync(int(f.Fd()))
}
