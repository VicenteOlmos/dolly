//go:build !windows && !darwin

package update

func darwinProcessExited(pid int) bool {
	return false
}
