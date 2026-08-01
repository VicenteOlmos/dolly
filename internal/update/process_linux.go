//go:build linux

package update

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processExited(pid int) bool {
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return true
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return false
			}
			return fields[1] == "Z"
		}
	}
	return false
}
