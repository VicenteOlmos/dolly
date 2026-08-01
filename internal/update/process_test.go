package update

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	t.Cleanup(func() { _ = cmd.Process.Kill() })

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

func TestReportDurabilityAtomicPublish(t *testing.T) {
	dir := t.TempDir()
	if err := writePendingReport(dir, pendingReport{
		Status:        StatusUpdated,
		RemoteVersion: "v0.3.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reportTempPath(dir)); !os.IsNotExist(err) {
		t.Fatal("temp report should not remain after publish")
	}
	data, err := os.ReadFile(reportPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "updated") {
		t.Fatalf("report = %s", string(data))
	}
}
