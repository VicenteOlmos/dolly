package clone

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func saveSeams() func() {
	prevAcquire := lockCacheAcquire
	prevRelease := lockCacheRelease
	prevClose := lockCacheClose
	prevContention := lockCacheContention
	prevNow := lockNow
	prevSleep := lockSleep
	return func() {
		lockCacheAcquire = prevAcquire
		lockCacheRelease = prevRelease
		lockCacheClose = prevClose
		lockCacheContention = prevContention
		lockNow = prevNow
		lockSleep = prevSleep
	}
}

func TestCacheLockContentionTimeout(t *testing.T) {
	restore := saveSeams()
	defer restore()

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	lockNow = func() time.Time {
		v := now
		now = now.Add(100 * time.Millisecond)
		return v
	}
	lockSleep = func(d time.Duration) {}

	contentionErr := errors.New("contention")
	acquireCalls := 0
	lockCacheAcquire = func(f *os.File) error {
		acquireCalls++
		return contentionErr
	}
	lockCacheContention = func(err error) bool { return errors.Is(err, contentionErr) }

	_, err := lockCacheFile(filepath.Join(t.TempDir(), "l", "test.lock"))
	if !errors.Is(err, errCacheLockTimeout) {
		t.Fatalf("expected errCacheLockTimeout, got %v", err)
	}
	if acquireCalls < 2 {
		t.Fatalf("expected >=2 acquire attempts (retried), got %d", acquireCalls)
	}
}

func TestCacheLockNonContentionImmediate(t *testing.T) {
	restore := saveSeams()
	defer restore()

	nonContentionErr := errors.New("permission denied")
	acquireCalls := 0
	lockCacheAcquire = func(f *os.File) error {
		acquireCalls++
		return nonContentionErr
	}
	lockCacheContention = func(err error) bool { return false }
	lockSleep = func(d time.Duration) { t.Error("sleep called for non-contention error") }

	_, err := lockCacheFile(filepath.Join(t.TempDir(), "l", "test.lock"))
	if acquireCalls != 1 {
		t.Fatalf("expected 1 acquire attempt, got %d", acquireCalls)
	}
	if errors.Is(err, errCacheLockTimeout) {
		t.Fatal("non-contention error should not return errCacheLockTimeout")
	}
	if !errors.Is(err, nonContentionErr) {
		t.Fatalf("expected %v, got %v", nonContentionErr, err)
	}
}

func TestCacheLockCloseReleaseOrdering(t *testing.T) {
	restore := saveSeams()
	defer restore()

	unlockErr := errors.New("unlock failed")
	closeErr := errors.New("close failed")
	var order []string

	lockCacheRelease = func(f *os.File) error { order = append(order, "unlock"); return unlockErr }
	lockCacheClose = func(f *os.File) error { order = append(order, "close"); return closeErr }

	f, err := os.CreateTemp(t.TempDir(), "lc")
	if err != nil {
		t.Fatal(err)
	}
	l := &cacheLock{f: f}
	got := l.close()

	if len(order) != 2 || order[0] != "unlock" || order[1] != "close" {
		t.Fatalf("expected unlock before close, got %v", order)
	}
	if !errors.Is(got, unlockErr) {
		t.Fatalf("expected first failing cause unlockErr, got %v", got)
	}
}

func TestCacheLockExcludeHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l", "test.lock")
	first, err := lockCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()

	_, err = lockCacheFile(path)
	if err == nil {
		t.Fatal("expected second acquire to fail while held")
	}
	if !errors.Is(err, errCacheLockTimeout) {
		t.Fatalf("expected errCacheLockTimeout, got %v", err)
	}
}

func TestCacheLockReacquireAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l", "test.lock")
	first, err := lockCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.close(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	second, err := lockCacheFile(path)
	if err != nil {
		t.Fatalf("reacquire after release failed: %v", err)
	}
	second.close()
}

func TestCacheLockRemainsBlockedWhileHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l", "test.lock")
	first, err := lockCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan error, 1)
	go func() {
		l, err := lockCacheFile(path)
		if err != nil {
			acquired <- err
			return
		}
		l.close()
		acquired <- nil
	}()

	time.Sleep(100 * time.Millisecond)
	select {
	case res := <-acquired:
		t.Fatalf("second acquire should not succeed while held: %v", res)
	default:
	}

	if err := first.close(); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	// Wait for goroutine; success or timeout both acceptable after release.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-acquired
	}()
	wg.Wait()
}
