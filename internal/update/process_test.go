package update

import (
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestWaitForPIDExitAlreadyGone(t *testing.T) {
	if err := defaultWaitForPIDExit(9999999, time.Second); err != nil {
		t.Fatalf("waitForPIDExit: %v", err)
	}
}

func TestWaitForPIDExitBlocksUntilChildExits(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess timing")
	}
	cmd := exec.Command("sleep", "0.2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() {
		defer close(reaped)
		_ = cmd.Wait()
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-reaped
	})

	start := time.Now()
	if err := defaultWaitForPIDExit(cmd.Process.Pid, 5*time.Second); err != nil {
		t.Fatalf("waitForPIDExit: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("returned too early after %v", elapsed)
	}
}

func TestWaitForPIDExitTimesOut(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	err := defaultWaitForPIDExit(cmd.Process.Pid, 100*time.Millisecond)
	if err == nil || err.Error() != "timed out waiting for pid "+strconv.Itoa(cmd.Process.Pid) {
		t.Fatalf("err = %v", err)
	}
}
