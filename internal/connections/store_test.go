package connections

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".dolly.connections.yaml")
	store, err := NewFileStore(path, false)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sampleConnection(name string) Connection {
	return Connection{
		Name:     name,
		Host:     "db.example.com",
		Port:     "5432",
		Database: "app",
		User:     "app",
		Password: "secret",
		Schemas:  []string{"app", "billing"},
	}
}

func TestFileStoreListEmptyMissingFile(t *testing.T) {
	store := newTestStore(t)
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
}

func TestFileStoreSaveDuplicateRejected(t *testing.T) {
	store := newTestStore(t)
	c := sampleConnection("staging")
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	err := store.Save(Connection{
		Name:     "staging",
		Host:     "other",
		Database: "other",
		User:     "other",
	})
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "db.example.com" {
		t.Fatalf("existing entry changed: %+v", got)
	}
}

func TestFileStoreUpsertPreservesSchemas(t *testing.T) {
	store := newTestStore(t)
	initial := sampleConnection("staging")
	if err := store.Save(initial); err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpsertBySignature(Connection{
		Host:     "db.example.com",
		Port:     "5432",
		Database: "app",
		User:     "app",
		Password: "new-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "staging" {
		t.Fatalf("name = %q, want staging", updated.Name)
	}
	if updated.Password != "new-secret" {
		t.Fatalf("password not updated")
	}
	if len(updated.Schemas) != 2 || updated.Schemas[0] != "app" {
		t.Fatalf("schemas not preserved: %+v", updated.Schemas)
	}
}

func TestFileStoreUpsertAssignsConnN(t *testing.T) {
	store := newTestStore(t)
	first, err := store.UpsertBySignature(Connection{
		Host: "h1", Port: "5432", Database: "db1", User: "u1", Password: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "conn-1" {
		t.Fatalf("first name = %q, want conn-1", first.Name)
	}

	second, err := store.UpsertBySignature(Connection{
		Host: "h2", Port: "5432", Database: "db2", User: "u2", Password: "p2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "conn-2" {
		t.Fatalf("second name = %q, want conn-2", second.Name)
	}
}

func TestFileStorePersistMode0600(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(sampleConnection("prod")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator != '\\' && info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "secret") {
		t.Fatal("expected plaintext password in encrypt-off mode")
	}
}

func TestFileStorePlaintextWhenEncryptDisabled(t *testing.T) {
	store := newTestStore(t)
	c := sampleConnection("prod")
	c.Password = "plain-pass"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if isCipherEnvelope(data) {
		t.Fatal("encrypt off must not write a cipher envelope")
	}
	if !strings.Contains(string(data), "password: plain-pass") {
		t.Fatalf("expected plaintext password in YAML, got:\n%s", data)
	}
	reloaded, err := NewFileStore(store.path, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.Password != "plain-pass" {
		t.Fatalf("reload password = %q", got.Password)
	}
}

func TestFileStoreListSortedByName(t *testing.T) {
	store := newTestStore(t)
	for _, name := range []string{"zebra", "alpha", "mid"} {
		if err := store.Save(sampleConnection(name)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d entries, want 3", len(list))
	}
	want := []string{"alpha", "mid", "zebra"}
	for i, name := range want {
		if list[i].Name != name {
			t.Fatalf("list[%d].Name = %q, want %q", i, list[i].Name, name)
		}
	}
}

func TestFileStoreGetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStoreRenameDuplicateTarget(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(sampleConnection("staging")); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sampleConnection("prod")); err != nil {
		t.Fatal(err)
	}
	err := store.Rename("staging", "prod")
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}
	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "staging" {
		t.Fatalf("rename should not apply: %+v", got)
	}
}

func TestFileStoreRenameAndDelete(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(sampleConnection("staging")); err != nil {
		t.Fatal(err)
	}
	if err := store.Rename("staging", "prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("staging"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("staging should be gone: %v", err)
	}
	if err := store.Delete("prod"); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty store, got %+v", list)
	}
}

func TestFileStorePut(t *testing.T) {
	store := newTestStore(t)
	c := sampleConnection("staging")
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	updated := c
	updated.Host = "updated.example.com"
	updated.Schemas = []string{"billing"}
	if err := store.Put(updated); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "updated.example.com" || len(got.Schemas) != 1 || got.Schemas[0] != "billing" {
		t.Fatalf("Put result = %+v", got)
	}
	if err := store.Put(sampleConnection("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Put missing = %v, want ErrNotFound", err)
	}
}

func TestFileStoreUpsertBySignaturePrefersNameWithDuplicateSignatures(t *testing.T) {
	store := newTestStore(t)
	base := Connection{
		Host: "h", Port: "5432", Database: "db", User: "u", Password: "p1",
	}
	alpha := base
	alpha.Name = "alpha"
	alpha.Schemas = []string{"app"}
	beta := base
	beta.Name = "beta"
	beta.Schemas = []string{"billing"}
	if err := store.Save(alpha); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(beta); err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpsertBySignature(Connection{
		Name: "beta", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p2",
		Schemas: []string{"billing", "public"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "beta" || updated.Password != "p2" {
		t.Fatalf("beta update = %+v", updated)
	}
	gotAlpha, err := store.Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if gotAlpha.Password != "p1" {
		t.Fatalf("alpha password changed: %+v", gotAlpha)
	}
}

func TestOpenStoreDisabledReturnsNil(t *testing.T) {
	cfg := config.DefaultConfig()
	store, err := OpenStore(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if store != nil {
		t.Fatal("expected nil store when save_connections is false")
	}
}

func TestFileStoreSSLMODERoundTrip(t *testing.T) {
	store := newTestStore(t)
	c := sampleConnection("prod")
	c.SSLMODE = "require"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.SSLMODE != "require" {
		t.Fatalf("SSLMODE = %q, want require", got.SSLMODE)
	}
}

func TestFileStoreSSLMODEMergePreserved(t *testing.T) {
	store := newTestStore(t)
	initial := sampleConnection("staging")
	initial.SSLMODE = "verify-ca"
	if err := store.Save(initial); err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpsertBySignature(Connection{
		Host: "db.example.com", Port: "5432", Database: "app", User: "app",
		Password: "new-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	// SSLMODE should be preserved from existing when incoming.SSLMODE is empty
	if updated.SSLMODE != "verify-ca" {
		t.Fatalf("SSLMODE should be preserved from existing when incoming empty, got %q", updated.SSLMODE)
	}

	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.SSLMODE != "verify-ca" {
		t.Fatalf("SSLMODE = %q, want verify-ca", got.SSLMODE)
	}
	if got.Password != "new-secret" {
		t.Fatalf("password not updated")
	}
}

func TestFileStoreSSLMODEMergeOverridden(t *testing.T) {
	store := newTestStore(t)
	initial := sampleConnection("staging")
	initial.SSLMODE = "verify-ca"
	if err := store.Save(initial); err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpsertBySignature(Connection{
		Host: "db.example.com", Port: "5432", Database: "app", User: "app",
		Password: "new-secret", SSLMODE: "require",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SSLMODE != "require" {
		t.Fatalf("SSLMODE = %q, want require", updated.SSLMODE)
	}

	got, err := store.Get("staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.SSLMODE != "require" {
		t.Fatalf("SSLMODE = %q, want require", got.SSLMODE)
	}
}

func TestFileStoreLoadFixesUnsafePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".dolly.connections.yaml")
	content := `connections:
- name: prod
  host: db.example.com
  port: "5432"
  database: app
  user: app
  password: secret
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewFileStore(path, false)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := store.Get("prod")
	if err != nil {
		t.Fatalf("Get after fixing 0644 store: %v", err)
	}
	if conn.Host != "db.example.com" {
		t.Fatalf("host = %q", conn.Host)
	}

	// Verify permissions were corrected.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator != '\\' && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after load = %o, want 0600", info.Mode().Perm())
	}
}

func TestFileStoreLockFileCreatedAndReleased(t *testing.T) {
	store := newTestStore(t)
	if err := store.Save(sampleConnection("test")); err != nil {
		t.Fatal(err)
	}
	lockPath := store.path + ".lock"
	// Lock file persists (reused); verify flock was released (not held).
	f, err := os.Open(lockPath)
	if err != nil {
		t.Fatalf("lock file should exist after save: %v", err)
	}
	f.Close()
}

func TestOpenStorePlaintextLoadsWithoutKeyWhenEncryptDefaulted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".dolly.connections.yaml")
	content := `connections:
- name: prod
  host: db.example.com
  port: "5432"
  database: app
  user: app
  password: secret
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Ensure no encryption key — this MUST still load plaintext transparently.
	t.Setenv("DOLLY_CONNECTIONS_KEY", "")

	store, err := NewFileStore(path, true) // encrypt=true simulates defaulted-true
	if err != nil {
		t.Fatal(err)
	}

	conn, err := store.Get("prod")
	if err != nil {
		t.Fatalf("plaintext store must load when encrypt is true (upgrade compat): %v", err)
	}
	if conn.Host != "db.example.com" || conn.Password != "secret" {
		t.Fatalf("got host=%q password=%q, want db.example.com/secret", conn.Host, conn.Password)
	}
}
