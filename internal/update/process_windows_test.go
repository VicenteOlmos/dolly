//go:build windows

package update

import (
	"errors"
	"testing"
	"time"

	gwindows "golang.org/x/sys/windows"
)

func TestWaitForPIDExitAccessDeniedFailsClosed(t *testing.T) {
	const systemPID = 4
	err := defaultWaitForPIDExit(systemPID, 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected access denied open failure")
	}
	if errors.Is(err, gwindows.ERROR_ACCESS_DENIED) {
		return
	}
	if err.Error() == "" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitForPIDExitInvalidParameterMeansExited(t *testing.T) {
	if err := defaultWaitForPIDExit(9999999, time.Second); err != nil {
		t.Fatalf("nonexistent pid should be treated as exited: %v", err)
	}
}
