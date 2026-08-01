package update

import (
	"fmt"
	"os"
	"os/exec"
)

var (
	startDetachedProcess = defaultStartDetachedProcess
	waitForProcessExit   = defaultWaitForProcessExit
	waitForPIDExit       = defaultWaitForPIDExit
	cleanupWaitParentPID = func() int { return os.Getppid() }
)

func defaultWaitForProcessExit(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	state, err := proc.Wait()
	if err != nil {
		return err
	}
	if !state.Success() {
		return fmt.Errorf("process %d exited with %s", pid, state)
	}
	return nil
}

func runCommand(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty argv")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
