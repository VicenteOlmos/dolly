package dumphistory

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestNextSeqEmptyBase(t *testing.T) {
	dir := t.TempDir()
	seq, err := NextSeq(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
}

func TestNextSeqFromDiskAndStore(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "2"), 0o755); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Register(Record{
		Seq:       4,
		BaseDir:   dir,
		Path:      filepath.Join(dir, "4"),
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	seq, err := NextSeq(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 5 {
		t.Fatalf("seq = %d, want 5", seq)
	}
}

func TestAllocateDir(t *testing.T) {
	dir := t.TempDir()
	path, seq, err := AllocateDir(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("seq = %d, want 1", seq)
	}
	want := filepath.Join(dir, "1")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestRegisterListBase(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	baseA := "/tmp/dumps-a"
	baseB := "/tmp/dumps-b"
	now := time.Now().UTC()
	if err := store.Register(Record{Seq: 1, BaseDir: baseA, Path: baseA + "/1", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(Record{Seq: 2, BaseDir: baseB, Path: baseB + "/1", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListBase(baseA)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Seq != 1 {
		t.Fatalf("ListBase = %+v, want one record for baseA", list)
	}
}

func TestFileStoreRegisterConcurrent(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	now := time.Now().UTC()
	errs := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- store.Register(Record{Seq: 1, BaseDir: "/tmp/a", Path: "/tmp/a/1", CreatedAt: now})
	}()
	go func() {
		defer wg.Done()
		errs <- store.Register(Record{Seq: 2, BaseDir: "/tmp/b", Path: "/tmp/b/1", CreatedAt: now.Add(time.Second)})
	}()
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
	}

	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d records, want 2 (no lost updates)", len(list))
	}
}

func TestFileStoreLoadFixesLoosePermissions(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(storePath, []byte(`{"records":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestFileStorePersistWrites0600(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "history.json")
	store, err := NewFileStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Register(Record{Seq: 1, BaseDir: "/tmp/a", Path: "/tmp/a/1", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
}
