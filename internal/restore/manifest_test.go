package restore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPartialStateManifest_atomicRewriteAndPermissions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested", "state")
	path := filepath.Join(dir, "partial-state.json")

	initial := NewPartialStateManifest([]string{"public.b", "public.a"})
	if err := WritePartialStateManifest(path, initial); err != nil {
		t.Fatal(err)
	}
	assertFileMode(t, path, partialStateFileMode)
	assertDirMode(t, dir, partialStateDirMode)

	if err := initial.MarkCommitted("public.a"); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialStateManifest(path, initial); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPartialStateManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Committed) != 1 || loaded.Committed[0] != "public.a" {
		t.Fatalf("committed = %#v", loaded.Committed)
	}
	if len(loaded.Pending) != 1 || loaded.Pending[0] != "public.b" {
		t.Fatalf("pending = %#v", loaded.Pending)
	}
}

func TestPartialStateManifest_deterministicOrder(t *testing.T) {
	m := NewPartialStateManifest([]string{"app.z", "public.a", "app.a", "public.a"})
	if got := strings.Join(m.Pending, ","); got != "app.a,app.z,public.a" {
		t.Fatalf("pending order = %q", got)
	}
	if err := m.MarkCommitted("public.a"); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkFailed("app.z", errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.Committed, ","); got != "public.a" {
		t.Fatalf("committed = %q", got)
	}
	if len(m.Failed) != 1 || m.Failed[0].Table != "app.z" {
		t.Fatalf("failed = %#v", m.Failed)
	}
}

func TestPartialStateManifest_redactionNoDSN(t *testing.T) {
	m := NewPartialStateManifest([]string{"public.users"})
	secret := "postgres://user:super-secret@localhost/db?password=query-secret"
	if err := m.MarkFailed("public.users", errors.New("copy failed for "+secret)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Failed[0].Error, "super-secret") || strings.Contains(m.Failed[0].Error, "query-secret") {
		t.Fatalf("failure detail leaked credentials: %q", m.Failed[0].Error)
	}
	if !strings.Contains(m.Failed[0].Error, "copy failed") {
		t.Fatalf("failure detail too generic: %q", m.Failed[0].Error)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := WritePartialStateManifest(path, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "super-secret") || strings.Contains(body, "password=query-secret") {
		t.Fatalf("manifest JSON leaked credentials: %s", body)
	}
	if strings.Contains(body, "dsn") || strings.Contains(body, "DSN") {
		t.Fatalf("manifest JSON must not include DSN fields: %s", body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"dsn", "password", "source_dsn", "target_dsn"} {
		if _, ok := parsed[forbidden]; ok {
			t.Fatalf("manifest must not include %q field", forbidden)
		}
	}
}

func TestPartialStateManifest_retainAndRemoveLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	m := NewPartialStateManifest([]string{"public.users"})
	if err := WritePartialStateManifest(path, m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := RemovePartialStateManifest(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected manifest removed, stat err = %v", err)
	}
}

func TestPartialStateManifest_writeFailureCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	m := NewPartialStateManifest([]string{"public.users"})

	origRename := partialStateRename
	t.Cleanup(func() { partialStateRename = origRename })
	partialStateRename = func(string, string) error {
		return errors.New("rename blocked")
	}

	if err := WritePartialStateManifest(path, m); err == nil {
		t.Fatal("expected write failure")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("left temp file behind: %s", entry.Name())
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("final manifest should not exist after failed write")
	}
}

func TestValidatePartialStatePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), partialStateDirMode); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePartialStatePath(""); !errors.Is(err, ErrPartialStatePath) {
		t.Fatalf("empty path err = %v", err)
	}
	if err := ValidatePartialStatePath(dir); !errors.Is(err, ErrPartialStatePath) {
		t.Fatalf("directory path err = %v", err)
	}
	if err := ValidatePartialStatePath(filepath.Join(dir, "ok.json")); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePartialStatePath(filepath.Join(dir, "nested", "ok.json")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../state.json", "../../state.json"} {
		if err := ValidatePartialStatePath(bad); !errors.Is(err, ErrPartialStatePath) {
			t.Fatalf("traversal path %q err = %v", bad, err)
		}
	}
	if got := DefaultPartialStatePath("/tmp/work"); got != filepath.Join("/tmp/work", defaultPartialState) {
		t.Fatalf("default path = %q", got)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("file mode = %o, want %o", info.Mode().Perm(), want)
	}
}

func assertDirMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("dir mode = %o, want %o", info.Mode().Perm(), want)
	}
}
