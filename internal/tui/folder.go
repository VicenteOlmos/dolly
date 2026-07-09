package tui

import (
	"os/exec"
	"runtime"
)

// FolderOpener opens a directory in the system file manager.
type FolderOpener interface {
	Open(path string) error
}

type defaultFolderOpener struct{}

func (defaultFolderOpener) Open(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
