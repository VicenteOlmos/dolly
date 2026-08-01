//go:build windows

package update

import "os"

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func syncOpenFile(f *os.File) error {
	return f.Sync()
}

func syncDir(string) error {
	return nil
}
