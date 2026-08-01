//go:build darwin

package update

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func processExited(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil {
		errno, ok := err.(syscall.Errno)
		return ok && errno == syscall.ESRCH
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return true
	}
	return kp.Proc.P_stat == 'Z'
}
