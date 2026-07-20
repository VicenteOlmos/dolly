//go:build windows

package dumphistory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func withHistoryLockSeams(t *testing.T, lock func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error) {
	t.Helper()
	origLock, origNow, origSleep := lockHistFileEx, histLockNow, histLockSleep
	lockHistFileEx = lock
	now := time.Unix(0, 0)
	histLockNow = func() time.Time { return now }
	histLockSleep = func(time.Duration) { now = now.Add(lockTimeout) }
	t.Cleanup(func() { lockHistFileEx, histLockNow, histLockSleep = origLock, origNow, origSleep })
}

func TestLockHistFileWaitsThenReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.lock")
	first, err := lockHistFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	acquired := make(chan *histLock, 1)
	fail := make(chan error, 1)
	go func() {
		lock, err := lockHistFile(path)
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
}

func TestLockHistFileTimeoutClosesHandleAndReacquires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.lock")
	withHistoryLockSeams(t, func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error {
		return windows.ERROR_LOCK_VIOLATION
	})
	started := histLockNow()
	_, err := lockHistFile(path)
	if !errors.Is(err, ErrLockTimeout) || histLockNow().Sub(started) < lockTimeout {
		t.Fatalf("err=%v elapsed=%s", err, histLockNow().Sub(started))
	}
	lockHistFileEx = windows.LockFileEx
	if lock, err := lockHistFile(path); err != nil {
		t.Fatalf("reacquire after timeout: %v", err)
	} else if err := lock.close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockHistFilePreservesNonContentionError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.lock")
	withHistoryLockSeams(t, func(windows.Handle, uint32, uint32, uint32, uint32, *windows.Overlapped) error {
		return windows.ERROR_ACCESS_DENIED
	})
	_, err := lockHistFile(path)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
