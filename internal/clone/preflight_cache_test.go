package clone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPermissionCacheKeyVersionBump(t *testing.T) {
	dsns := preflightDSNs{
		sourceDSN: "postgres://u:p@h-a:5432/db_src",
		targetDSN: "postgres://u:p@h-a:5432/db_clone",
		sourceDB:  "db_src",
		sameInst:  true,
	}
	opts := Options{CloneName: "db_clone", SkipCreate: true}

	v5, err := permissionCacheKey(dsns, opts, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}

	src, err := parseDSNIdentity(dsns.sourceDSN)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := parseDSNIdentity(dsns.targetDSN)
	if err != nil {
		t.Fatal(err)
	}
	v4Payload := fmt.Sprintf(
		"check_version=4\nstrategy=%s\nskip_create=%t\nclone=%s\nsource=%s:%s:%s:%s\ntarget=%s:%s:%s\nsame=%t",
		"schema-replay",
		opts.SkipCreate,
		opts.CloneName,
		src.host, src.port, src.db, src.user,
		tgt.host, tgt.port, tgt.db,
		dsns.sameInst,
	)
	sum := sha256.Sum256([]byte(v4Payload))
	v4 := hex.EncodeToString(sum[:])

	if v4 == v5 {
		t.Fatalf("expected v4 and v5 cache keys to differ, both %q", v5)
	}
}

func TestPermissionCacheKeyStable(t *testing.T) {
	dsns := preflightDSNs{
		sourceDSN: "postgres://u:p@h-a:5432/db_src",
		targetDSN: "postgres://u:p@h-a:5432/db_clone",
		adminDSN:  "postgres://u:p@h-a:5432/postgres",
		sourceDB:  "db_src",
		sameInst:  true,
	}
	opts := Options{CloneName: "db_clone", SkipCreate: true}
	k1, err := permissionCacheKey(dsns, opts, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := permissionCacheKey(dsns, opts, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 || len(k1) != 64 {
		t.Fatalf("expected stable 64-char hex key, got %q", k1)
	}
}

func TestPermissionCacheStoreAndHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "permissions-cache.yaml")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	permissionCacheNow = func() time.Time { return now }
	defer func() { permissionCacheNow = time.Now }()

	cfg := PermissionCacheConfig{Enabled: true, Path: path, TTL: time.Hour}
	dsns := preflightDSNs{
		sourceDSN: "postgres://u:p@h-a:5432/db_src",
		targetDSN: "postgres://u:p@h-a:5432/db_clone",
		sourceDB:  "db_src",
		sameInst:  true,
	}
	opts := Options{CloneName: "db_clone", SkipCreate: true}
	key, err := permissionCacheKey(dsns, opts, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := buildPermissionCacheEntry(key, dsns, opts, "schema-replay", "app_user", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(cfg, entry); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Dir(path)
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir mode = %o, want 0700", info.Mode().Perm())
	}

	got, hit, err := lookupPermissionCache(cfg, key, now.Add(30*time.Minute))
	if err != nil || !hit {
		t.Fatalf("expected cache hit, hit=%v err=%v", hit, err)
	}
	if got.Role != "app_user" {
		t.Fatalf("role %q", got.Role)
	}

	_, hit, err = lookupPermissionCache(cfg, key, now.Add(2*time.Hour))
	if err != nil || hit {
		t.Fatalf("expected expired miss, hit=%v err=%v", hit, err)
	}
}

func TestPreflightSkipsPermissionQueriesOnCacheHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	permissionCacheNow = func() time.Time { return now }
	defer func() { permissionCacheNow = time.Now }()

	sourceDSN := "postgres://u:p@h-a:5432/db_src"
	cloneName := "db_clone"
	dsns := preflightDSNs{
		sourceDSN: sourceDSN,
		targetDSN: "postgres://u:p@h-a:5432/db_clone",
		sourceDB:  "db_src",
		sameInst:  true,
	}
	opts := Options{
		SourceDSN:  sourceDSN,
		CloneName:  cloneName,
		SkipCreate: true,
		Strategy:   "schema-replay",
		PermissionCache: PermissionCacheConfig{
			Enabled: true,
			Path:    path,
			TTL:     time.Hour,
		},
	}
	key, err := permissionCacheKey(dsns, opts, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := buildPermissionCacheEntry(key, dsns, opts, "schema-replay", "u", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(opts.PermissionCache, entry); err != nil {
		t.Fatal(err)
	}

	mockDB, mock := newSQLMock(t)
	defer mockDB.Close()

	origOpen := sqlOpenDB
	sqlOpenDB = func(dsn string) (*sql.DB, error) { return mockDB, nil }
	defer func() { sqlOpenDB = origOpen }()

	origLook := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/x", nil }
	defer func() { lookPath = origLook }()

	origDump := pgDumpVersion
	pgDumpVersion = func() (string, error) { return "pg_dump (PostgreSQL) 15.0", nil }
	defer func() { pgDumpVersion = origDump }()

	mock.ExpectPing()
	mock.ExpectQuery(`SHOW server_version_num`).
		WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(150002))

	if err := Preflight(context.Background(), opts, &SchemaReplayStrategy{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPermissionCacheSecureExistingCacheRead(t *testing.T) {
	t.Run("broad mode tightened before parse", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
		if err := os.WriteFile(path, []byte("entries: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPermissionCacheFile(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
		}
	})

	t.Run("tighten error beats parse error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
		if err := os.WriteFile(path, []byte("not: valid: yaml: [\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		orig := ensureCacheOwnerOnlyImpl
		ensureCacheOwnerOnlyImpl = func(string) error {
			return fmt.Errorf("injected tighten failure")
		}
		t.Cleanup(func() { ensureCacheOwnerOnlyImpl = orig })

		_, err := loadPermissionCacheFile(path)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "injected tighten failure") {
			t.Fatalf("expected tighten error, got %v", err)
		}
		if strings.Contains(err.Error(), "parse permission cache") {
			t.Fatalf("parse error should not win over tighten: %v", err)
		}
	})

	t.Run("invalid yaml fails closed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
		if err := os.WriteFile(path, []byte("not: valid: yaml: [\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPermissionCacheFile(path); err == nil || !strings.Contains(err.Error(), "parse permission cache") {
			t.Fatalf("expected parse failure, got %v", err)
		}
	})

	t.Run("read failure fails closed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "dir")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPermissionCacheFile(path); err == nil || !strings.Contains(err.Error(), "read permission cache") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})
}

func TestPermissionCacheMergePreservesLiveEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	permissionCacheNow = func() time.Time { return now }
	t.Cleanup(func() { permissionCacheNow = time.Now })

	ds := preflightDSNs{
		sourceDSN: "postgres://u:p@h:5432/db",
		targetDSN: "postgres://u:p@h:5432/clone",
		sourceDB:  "db",
		sameInst:  true,
	}
	cfg := PermissionCacheConfig{Enabled: true, Path: path, TTL: time.Hour}

	liveKey, err := permissionCacheKey(ds, Options{CloneName: "clone-live"}, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	liveEntry, err := buildPermissionCacheEntry(liveKey, ds, Options{CloneName: "clone-live"}, "schema-replay", "live-role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expiredEntry := liveEntry
	expiredEntry.Key = "expired-key"
	expiredEntry.ExpiresAt = now
	newKey, err := permissionCacheKey(ds, Options{CloneName: "clone-new"}, "schema-replay")
	if err != nil {
		t.Fatal(err)
	}
	sameKeyEntry, err := buildPermissionCacheEntry(newKey, ds, Options{CloneName: "clone-new"}, "schema-replay", "old-role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sameKeyEntry.Role = "old-role"

	seed := permissionCacheDoc{Entries: []permissionCacheEntry{liveEntry, expiredEntry, sameKeyEntry}}
	if err := savePermissionCacheFile(path, seed); err != nil {
		t.Fatal(err)
	}

	newEntry, err := buildPermissionCacheEntry(newKey, ds, Options{CloneName: "clone-new"}, "schema-replay", "new-role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(cfg, newEntry); err != nil {
		t.Fatal(err)
	}

	doc, err := loadPermissionCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]permissionCacheEntry, len(doc.Entries))
	for _, e := range doc.Entries {
		keys[e.Key] = e
	}
	if _, ok := keys[expiredEntry.Key]; ok {
		t.Fatalf("expired entry %q should be removed", expiredEntry.Key)
	}
	if got := keys[liveKey]; got.Role != "live-role" {
		t.Fatalf("live entry role = %q, want live-role", got.Role)
	}
	if got := keys[newKey]; got.Role != "new-role" {
		t.Fatalf("new entry role = %q, want new-role", got.Role)
	}
	if _, ok := keys[liveKey]; !ok {
		t.Fatal("expected live key to remain")
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Entries))
	}
}

var errInjectedReplacementFailure = fmt.Errorf("injected replacement failure")

func TestPermissionCacheReplacementFailurePreservesBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
	orig := []byte("entries: []\n")
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	permissionCacheNow = func() time.Time { return now }
	t.Cleanup(func() { permissionCacheNow = time.Now })

	origReplace := replacePermissionCacheFile
	replacePermissionCacheFile = func(src, dst string) error {
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("staging mode = %o, want 0600", info.Mode().Perm())
		}
		if info.Size() == 0 {
			return fmt.Errorf("staging file not written before replace")
		}
		staged, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if !bytes.Contains(staged, []byte("entries:")) || !bytes.Contains(staged, []byte("role: role")) {
			return fmt.Errorf("staging bytes incomplete or invalid")
		}
		return errInjectedReplacementFailure
	}
	t.Cleanup(func() { replacePermissionCacheFile = origReplace })

	var warnings []string
	origWarn := warnPermissionCache
	warnPermissionCache = func(msg string) { warnings = append(warnings, msg) }
	t.Cleanup(func() { warnPermissionCache = origWarn })
	ds := preflightDSNs{
		sourceDSN: "postgres://u:p@h:5432/db",
		targetDSN: "postgres://u:p@h:5432/clone",
		sourceDB:  "db",
		sameInst:  true,
	}
	entry, err := buildPermissionCacheEntry("k", ds, Options{CloneName: "c"}, "schema-replay", "role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(PermissionCacheConfig{Enabled: true, Path: path, TTL: time.Hour}, entry); err != nil {
		t.Fatalf("store should bypass write failure: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "injected replacement failure") {
		t.Fatalf("expected persist warning wrapping injected failure, got %v", warnings)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, orig) {
		t.Fatalf("target bytes changed: %q", got)
	}

	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".dolly.permissions-cache-") {
			t.Fatalf("temp residue %q", e.Name())
		}
	}
}

func TestPermissionCacheWindowsRuntime(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows runtime harness only")
	}
	path := filepath.Join(t.TempDir(), "permissions-cache.yaml")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	permissionCacheNow = func() time.Time { return now }
	t.Cleanup(func() { permissionCacheNow = time.Now })

	if err := ensureCacheOwnerOnly(path); err != nil {
		t.Fatalf("permission tighten should be no-op on Windows: %v", err)
	}

	ds := preflightDSNs{
		sourceDSN: "postgres://u:p@h:5432/db",
		targetDSN: "postgres://u:p@h:5432/clone",
		sourceDB:  "db",
		sameInst:  true,
	}
	cfg := PermissionCacheConfig{Enabled: true, Path: path, TTL: time.Hour}
	entry, err := buildPermissionCacheEntry("win-live", ds, Options{CloneName: "win-live"}, "schema-replay", "live-role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(cfg, entry); err != nil {
		t.Fatal(err)
	}
	entry, err = buildPermissionCacheEntry("win-new", ds, Options{CloneName: "win-new"}, "schema-replay", "new-role", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := storePermissionCache(cfg, entry); err != nil {
		t.Fatal(err)
	}
	if doc, err := loadPermissionCacheFile(path); err != nil || len(doc.Entries) != 2 {
		t.Fatalf("entries after second store: err=%v len=%d", err, len(doc.Entries))
	}
}
