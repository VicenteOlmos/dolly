//go:build !windows && !linux && !darwin

package update

import (
	"syscall"
)

func processExited(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return false
	}
	errno, ok := err.(syscall.Errno)
	return ok && errno == syscall.ESRCH
}
