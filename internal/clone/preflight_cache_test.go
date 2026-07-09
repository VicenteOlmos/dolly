package clone

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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
