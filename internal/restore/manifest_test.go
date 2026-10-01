package restore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/testutil"
)

func TestPartialStateTargetFromConninfo(t *testing.T) {
	got := PartialStateTargetFromConninfo("postgres://user:secret@db.example:5433/app")
	want := PartialStateTarget{Host: "db.example", Port: "5433", Database: "app"}
	if !got.same(want) {
		t.Fatalf("url target = %+v", got)
	}
	got = PartialStateTargetFromConninfo("host=db.example port=5432 dbname=app password=secret")
	want = PartialStateTarget{Host: "db.example", Port: "5432", Database: "app"}
	if !got.same(want) {
		t.Fatalf("keyword target = %+v", got)
	}
	if !PartialStateTargetFromConninfo("").empty() {
		t.Fatal("empty conninfo should have an empty target")
	}
	quoted := PartialStateTargetFromConninfo(`host='/tmp/pg one' dbname=app`)
	if quoted.Host != "/tmp/pg one" || quoted.Database != "app" {
		t.Fatalf("quoted target = %+v", quoted)
	}
	if PartialStateTargetFromConninfo("service=missing_service_name").same(PartialStateTargetFromConninfo("service=another_missing_service")) {
		t.Fatal("unresolved services must not reuse state")
	}
	if !PartialStateTargetFromConninfo("host=a,b dbname=app").empty() {
		t.Fatal("failover host must not reuse state")
	}
}

func TestPartialStateTargetFromService(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pg_service.conf")
	if err := os.WriteFile(path, []byte("[one]\nhost=first.example\ndbname=app\n[two]\nhost=second.example\ndbname=app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGSERVICEFILE", path)
	one := PartialStateTargetFromConninfo("service=one")
	two := PartialStateTargetFromConninfo("service=two")
	if one.empty() || two.empty() || one.same(two) {
		t.Fatalf("services collapsed: one=%+v two=%+v", one, two)
	}
}

func TestPartialStateTableSetFingerprintStable(t *testing.T) {
	a := partialStateTableSetFingerprint([]string{"public.b", "public.a"})
	b := partialStateTableSetFingerprint([]string{"public.a", "public.b"})
	if a == "" || a != b {
		t.Fatalf("fingerprints = %q %q", a, b)
	}
	if partialStateTableSetFingerprint([]string{"public.a"}) == a {
		t.Fatal("different table sets must not share fingerprint")
	}
}

func TestMergePartialStateManifestForRetry(t *testing.T) {
	existing := PartialStateManifest{
		Committed: []string{"public.users"},
		Failed:    []PartialStateFailure{{Table: "public.posts", Error: "copy failed"}},
		Pending:   nil,
	}
	target := PartialStateTarget{Host: "localhost", Port: "5432", Database: "db"}
	got := mergePartialStateManifestForRetry(existing, []string{"public.posts", "public.users", "public.comments"}, target)
	if !got.Target.same(target) {
		t.Fatalf("target = %+v", got.Target)
	}
	if !reflect.DeepEqual(got.Committed, []string{"public.users"}) {
		t.Fatalf("committed = %v", got.Committed)
	}
	if len(got.Failed) != 0 {
		t.Fatalf("failed = %v, want cleared on retry", got.Failed)
	}
	if !reflect.DeepEqual(got.Pending, []string{"public.comments", "public.posts"}) {
		t.Fatalf("pending = %v", got.Pending)
	}
	if got.TableSetFingerprint != partialStateTableSetFingerprint([]string{"public.posts", "public.users", "public.comments"}) {
		t.Fatalf("fingerprint = %q", got.TableSetFingerprint)
	}
}

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
	target := filepath.Join(dir, "state.json")
	if err := os.Symlink(target, filepath.Join(dir, "state-link.json")); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePartialStatePath(filepath.Join(dir, "state-link.json")); !errors.Is(err, ErrPartialStatePath) {
		t.Fatalf("symlink path err = %v", err)
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
	testutil.AssertFilePerm(t, info.Mode(), want, "file mode")
}

func assertDirMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertFilePerm(t, info.Mode(), want, "dir mode")
}
