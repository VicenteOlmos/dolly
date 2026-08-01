//go:build windows

package update

import (
	"fmt"
	"os"
	"syscall"
	"time"

	gwindows "golang.org/x/sys/windows"
)

func defaultStartDetachedProcess(path string, argv []string) error {
	attr := &os.ProcAttr{
		Dir:   filepathDir(path),
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys: &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		},
	}
	proc, err := os.StartProcess(path, argv, attr)
	if err != nil {
		return err
	}
	_ = proc.Release()
	return nil
}

func defaultWaitForPIDExit(pid int, timeout time.Duration) error {
	if pid <= 0 {
		return nil
	}
	handle, err := gwindows.OpenProcess(gwindows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errno, ok := err.(gwindows.Errno); ok && errno == gwindows.ERROR_INVALID_PARAMETER {
			return nil
		}
		return fmt.Errorf("open pid %d: %w", pid, err)
	}
	defer gwindows.CloseHandle(handle)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r, err := gwindows.WaitForSingleObject(handle, 0)
		if err != nil {
			return fmt.Errorf("wait for pid %d: %w", pid, err)
		}
		switch r {
		case uint32(gwindows.WAIT_OBJECT_0):
			return nil
		case uint32(gwindows.WAIT_TIMEOUT):
			time.Sleep(50 * time.Millisecond)
		default:
			return fmt.Errorf("unexpected wait result %d for pid %d", r, pid)
		}
	}
	return fmt.Errorf("timed out waiting for pid %d", pid)
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			if i == 0 {
				return path[:1]
			}
			return path[:i]
		}
	}
	return "."
}
