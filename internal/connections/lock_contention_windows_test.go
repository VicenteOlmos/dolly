//go:build windows

package connections

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func withConnectionLockSeams(t *testing.T, lock func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error) {
	t.Helper()
	origLock, origNow, origSleep := lockFileEx, lockNow, lockSleep
	lockFileEx = lock
	now := time.Unix(0, 0)
	lockNow = func() time.Time { return now }
	lockSleep = func(time.Duration) { now = now.Add(lockTimeout) }
	t.Cleanup(func() { lockFileEx, lockNow, lockSleep = origLock, origNow, origSleep })
}

func TestLockFileWaitsThenReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.lock")
	first, err := lockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	acquired := make(chan *connLock, 1)
	fail := make(chan error, 1)
	go func() {
		lock, err := lockFile(path)
		if err != nil {
			fail <- err
			return
		}
		acquired <- lock
	}()
	select {
	case <-acquired:
		t.Fatal("second holder acquired before release")
	case err := <-fail:
		t.Fatal(err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := first.close(); err != nil {
		t.Fatal(err)
	}
	second := <-acquired
	if err := second.close(); err != nil {
		t.Fatal(err)
	}
	if third, err := lockFile(path); err != nil {
		t.Fatal(err)
	} else if err := third.close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockFileTimeoutClosesHandleAndReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.lock")
	withConnectionLockSeams(t, func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error {
		return windows.ERROR_LOCK_VIOLATION
	})
	started := lockNow()
	_, err := lockFile(path)
	if !errors.Is(err, ErrLockTimeout) || lockNow().Sub(started) < lockTimeout {
		t.Fatalf("err=%v elapsed=%s", err, lockNow().Sub(started))
	}
	lockFileEx = windows.LockFileEx
	if lock, err := lockFile(path); err != nil {
		t.Fatalf("reacquire after timeout: %v", err)
	} else if err := lock.close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockFilePreservesNonContentionError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.lock")
	withConnectionLockSeams(t, func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error {
		return windows.ERROR_ACCESS_DENIED
	})
	_, err := lockFile(path)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
