//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func defaultStartDetachedProcess(path string, argv []string) error {
	attr := &os.ProcAttr{
		Dir:   filepathDir(path),
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys: &syscall.SysProcAttr{
			Setsid: true,
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
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if processExited(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for pid %d", pid)
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return path[:1]
			}
			return path[:i]
		}
	}
	return "."
}
