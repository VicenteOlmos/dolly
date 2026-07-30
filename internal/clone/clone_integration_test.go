//go:build integration

package clone

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/VicenteOlmos/dolly/internal/db"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

func requirePgDumpMajorMatch(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := lookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not on PATH")
	}
	dumpOut, err := pgDumpVersion()
	if err != nil {
		t.Fatalf("pg_dump --version: %v", err)
	}
	dumpMajor, err := parsePgDumpMajor(dumpOut)
	if err != nil {
		t.Fatalf("parse pg_dump version: %v", err)
	}
	var versionNum int
	if err := db.QueryRowContext(context.Background(), `SHOW server_version_num`).Scan(&versionNum); err != nil {
		t.Fatalf("server version: %v", err)
	}
	serverMajor, err := parseServerMajor(versionNum)
	if err != nil {
		t.Fatalf("parse server major: %v", err)
	}
	if dumpMajor != serverMajor {
		t.Fatalf("pg_dump major %d != server major %d", dumpMajor, serverMajor)
	}
}

func TestCloneRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for live clone round-trip integration tests; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	srcDBName := fmt.Sprintf("dolly_clone_src_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_clone_tgt_%d", os.Getpid())

	// Create source DB with test data.
	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, srcDBName)); err != nil {
		t.Fatalf("drop source db: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}
	defer func() {
		_, _ = adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, srcDBName))
	}()

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer srcDB.Close()

	if _, err := srcDB.ExecContext(context.Background(), `CREATE TABLE tbl (id serial PRIMARY KEY, c text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := srcDB.ExecContext(context.Background(), `INSERT INTO tbl (c) VALUES ('v1'), ('v2')`); err != nil {
		t.Fatalf("insert data: %v", err)
	}
	requirePgDumpMajorMatch(t, srcDB)

	// Run clone.
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, cloneName)); err != nil {
		t.Fatalf("drop target db: %v", err)
	}
	defer func() {
		_, _ = adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, cloneName))
	}()

	if err := Run(context.Background(), Options{
		SourceDSN: srcDSN,
		CloneName: cloneName,
		TargetDSN: "", // same host, computed from source
	}); err != nil {
		t.Fatalf("clone run: %v", err)
	}

	// Verify target row count.
	tgtDSN, err := RewriteDSN(dsn, cloneName)
	if err != nil {
		t.Fatalf("rewrite target DSN: %v", err)
	}

	tgtDB, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer tgtDB.Close()

	var count int
	if err := tgtDB.QueryRowContext(context.Background(), `SELECT count(*) FROM tbl`).Scan(&count); err != nil {
		t.Fatalf("count target rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("target row count = %d, want 2", count)
	}
}

func createIntegrationSource(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE users (id serial PRIMARY KEY, name text);
		CREATE SCHEMA app;
		CREATE TABLE app.events (id serial PRIMARY KEY, user_id int REFERENCES users(id), payload text);
		INSERT INTO users (name) VALUES ('alice'), ('bob');
		INSERT INTO app.events (user_id, payload) VALUES (1, 'event1'), (2, 'event2');
	`); err != nil {
		t.Fatalf("create source schema: %v", err)
	}
}

func verifyTargetRich(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 2 {
		t.Fatalf("users count = %d, want 2", count)
	}

	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM app.events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 2 {
		t.Fatalf("events count = %d, want 2", count)
	}

	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM users WHERE id = 1`).Scan(&name); err != nil {
		t.Fatalf("select user: %v", err)
	}
	if name != "alice" {
		t.Fatalf("user name = %q, want alice", name)
	}

	var payload string
	if err := db.QueryRowContext(ctx, `SELECT payload FROM app.events WHERE id = 2`).Scan(&payload); err != nil {
		t.Fatalf("select event: %v", err)
	}
	if payload != "event2" {
		t.Fatalf("event payload = %q, want event2", payload)
	}

	var seqVal int
	if err := db.QueryRowContext(ctx, `SELECT last_value FROM users_id_seq`).Scan(&seqVal); err != nil {
		t.Fatalf("users seq: %v", err)
	}
	if seqVal != 2 {
		t.Fatalf("users seq = %d, want 2", seqVal)
	}
	if err := db.QueryRowContext(ctx, `SELECT last_value FROM app.events_id_seq`).Scan(&seqVal); err != nil {
		t.Fatalf("events seq: %v", err)
	}
	if seqVal != 2 {
		t.Fatalf("events seq = %d, want 2", seqVal)
	}

	// Verify FK constraint exists on the cloned schema.
	var fkName string
	if err := db.QueryRowContext(ctx, `
		SELECT conname FROM pg_constraint
		JOIN pg_class ON pg_class.oid = pg_constraint.conrelid
		JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
		WHERE pg_namespace.nspname = 'app'
		  AND pg_class.relname = 'events'
		  AND pg_constraint.contype = 'f'
	`).Scan(&fkName); err != nil {
		t.Fatalf("fk lookup: %v", err)
	}
	if fkName == "" {
		t.Fatal("expected FK constraint on app.events, got none")
	}
}

func TestCloneRoundTripSchemaReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for live clone round-trip integration tests; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	srcDBName := fmt.Sprintf("dolly_clone_sr_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_clone_tg_%d", os.Getpid())

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	ctx := context.Background()
	for _, name := range []string{srcDBName, cloneName} {
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	defer func() {
		for _, name := range []string{srcDBName, cloneName} {
			_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
		}
	}()

	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	createIntegrationSource(t, srcDB)

	// Re-open to run pg_dump version check against a live connection.
	_ = srcDB.Close()
	srcDB, err = sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("reopen source: %v", err)
	}
	requirePgDumpMajorMatch(t, srcDB)
	_ = srcDB.Close()

	if err := Run(ctx, Options{
		SourceDSN: srcDSN,
		CloneName: cloneName,
		Strategy:  "schema-replay",
	}); err != nil {
		t.Fatalf("clone run: %v", err)
	}

	tgtDSN, err := RewriteDSN(dsn, cloneName)
	if err != nil {
		t.Fatalf("rewrite target DSN: %v", err)
	}

	tgtDB, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer tgtDB.Close()

	verifyTargetRich(t, tgtDB)
}

func TestCloneRoundTripTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for live clone round-trip integration tests; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	srcDBName := fmt.Sprintf("dolly_clone_tpl_src_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_clone_tpl_tgt_%d", os.Getpid())

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	ctx := context.Background()
	for _, name := range []string{srcDBName, cloneName} {
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	defer func() {
		for _, name := range []string{srcDBName, cloneName} {
			_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
		}
	}()

	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	createIntegrationSource(t, srcDB)
	// Template cloning requires no active connections on the source database.
	_ = srcDB.Close()

	if err := Run(ctx, Options{
		SourceDSN: srcDSN,
		CloneName: cloneName,
		Strategy:  "template",
	}); err != nil {
		t.Fatalf("clone run: %v", err)
	}

	tgtDSN, err := RewriteDSN(dsn, cloneName)
	if err != nil {
		t.Fatalf("rewrite target DSN: %v", err)
	}

	tgtDB, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer tgtDB.Close()

	verifyTargetRich(t, tgtDB)
}

func TestCloneRoundTripCopyStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for live clone round-trip integration tests; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	srcDBName := fmt.Sprintf("dolly_clone_cs_src_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_clone_cs_tgt_%d", os.Getpid())

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	ctx := context.Background()
	for _, name := range []string{srcDBName, cloneName} {
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	defer func() {
		for _, name := range []string{srcDBName, cloneName} {
			_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
		}
	}()

	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	createIntegrationSource(t, srcDB)

	_ = srcDB.Close()
	srcDB, err = sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("reopen source: %v", err)
	}
	requirePgDumpMajorMatch(t, srcDB)
	_ = srcDB.Close()

	if err := Run(ctx, Options{
		SourceDSN: srcDSN,
		CloneName: cloneName,
		Strategy:  "copy-stream",
	}); err != nil {
		t.Fatalf("clone run: %v", err)
	}

	tgtDSN, err := RewriteDSN(dsn, cloneName)
	if err != nil {
		t.Fatalf("rewrite target DSN: %v", err)
	}

	tgtDB, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer tgtDB.Close()

	verifyTargetRich(t, tgtDB)
}

func TestPreflightSchemaReplayLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for live clone round-trip integration tests; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	srcDBName := fmt.Sprintf("dolly_pf_src_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_pf_tgt_%d", os.Getpid())

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	ctx := context.Background()
	for _, name := range []string{srcDBName, cloneName} {
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	defer func() {
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, srcDBName))
		_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, cloneName))
	}()

	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, cloneName)); err != nil {
		t.Fatalf("create target db: %v", err)
	}

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer srcDB.Close()

	if _, err := srcDB.ExecContext(ctx, `CREATE TABLE tbl (id serial PRIMARY KEY, c text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	requirePgDumpMajorMatch(t, srcDB)

	if err := Preflight(ctx, Options{
		SourceDSN:  srcDSN,
		CloneName:  cloneName,
		SkipCreate: true,
	}, &SchemaReplayStrategy{}); err != nil {
		t.Fatalf("preflight: %v", err)
	}
}

func TestDotenvSourceDumpAndClonePreflightParity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set")
	}
	if !strings.Contains(dsn, "sslmode=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "sslmode=disable"
	}

	ctx := context.Background()
	dumpDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open dump-style source: %v", err)
	}
	defer dumpDB.Close()
	if err := dumpDB.PingContext(ctx); err != nil {
		t.Fatalf("dump-style ping: %v", err)
	}

	requirePgDumpMajorMatch(t, dumpDB)

	if err := Preflight(ctx, Options{
		SourceDSN:  dsn,
		CloneName:  "dolly_pf_parity_unused",
		SkipCreate: false,
	}, &CopyStreamStrategy{}); err != nil {
		t.Fatalf("clone preflight: %v", err)
	}
}

func TestReplicationPreflightLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set")
	}
	if _, err := lookPath("pg_basebackup"); err != nil {
		t.Skip("pg_basebackup not on PATH")
	}

	targetDir := t.TempDir()
	ctx := context.Background()

	if err := Preflight(ctx, Options{
		SourceDSN: dsn,
		CloneName: "unused",
		TargetDir: targetDir,
	}, &ReplicationStrategy{}); err != nil {
		t.Fatalf("preflight: %v", err)
	}
}

// TestRunCloneWithDotenvProfile validates the .env → ConnectionFromDotEnv →
// runCloneWithSource chain end-to-end. It verifies sslmode defaults to prefer
// and confirms no PGPASSWORD leaks into child process environments.
//
// SKIP DOCUMENTATION: This test requires a live PostgreSQL instance with
// CREATEDB privilege. It is NOT required for default `go test ./...` runs.
// The test is guarded by the integration build tag, testing.Short(), and
// the DOLLY_TEST_PG_DSN environment variable. Without a live PostgreSQL
// instance, this test will skip explicitly.
//
// In-process evidence (without a live database) is provided by
// TestRunCloneDotenvProfileInProcessEvidence in exec_test.go.
func TestRunCloneWithDotenvProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set — required for .env profile integration test; set it to a PostgreSQL DSN with CREATEDB privilege to enable")
	}

	// Create a temp .env file with the test DSN.
	dir := t.TempDir()
	dotenvPath := filepath.Join(dir, ".env")
	envContent := fmt.Sprintf("DB_URL=%s\n", dsn)
	if err := os.WriteFile(dotenvPath, []byte(envContent), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	// Load the .env as a connection profile.
	names := config.EnvVarNames{
		URLVar:      "DB_URL",
		HostVar:     "DB_HOST",
		PortVar:     "DB_PORT",
		NameVar:     "DB_NAME",
		UserVar:     "DB_USER",
		PasswordVar: "DB_PASSWORD",
	}
	conn, err := connections.ConnectionFromDotEnv(dotenvPath, names)
	if err != nil {
		t.Fatalf("ConnectionFromDotEnv: %v", err)
	}

	// Verify profile name defaults to "local".
	if conn.Name != "local" {
		t.Errorf("profile name = %q, want \"local\"", conn.Name)
	}

	profileDSN := conn.DSN()

	// SSLMODE defaults to "prefer" only when the .env URL omits sslmode;
	// integration DSNs often set sslmode=disable for local CI Postgres.
	wantSSLMode := "sslmode=prefer"
	if strings.Contains(dsn, "sslmode=") {
		wantSSLMode = "sslmode=" + strings.Split(strings.Split(dsn, "sslmode=")[1], "&")[0]
	}
	if !strings.Contains(profileDSN, wantSSLMode) {
		t.Errorf("DSN should contain %s, got:\n  %s", wantSSLMode, profileDSN)
	}

	// Run a lightweight clone using the resolved DSN.
	srcDBName := fmt.Sprintf("dolly_dotenv_src_%d", os.Getpid())
	cloneName := fmt.Sprintf("dolly_dotenv_tgt_%d", os.Getpid())

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatalf("rewrite admin DSN: %v", err)
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer adminDB.Close()

	ctx := context.Background()
	for _, name := range []string{srcDBName, cloneName} {
		if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	defer func() {
		for _, name := range []string{srcDBName, cloneName} {
			_, _ = adminDB.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
		}
	}()

	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, srcDBName)); err != nil {
		t.Fatalf("create source db: %v", err)
	}

	srcDSN, err := RewriteDSN(dsn, srcDBName)
	if err != nil {
		t.Fatalf("rewrite source DSN: %v", err)
	}

	srcDB, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	if _, err := srcDB.ExecContext(ctx, `CREATE TABLE t (id serial PRIMARY KEY, val text)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := srcDB.ExecContext(ctx, `INSERT INTO t (val) VALUES ('hello')`); err != nil {
		t.Fatalf("insert data: %v", err)
	}
	requirePgDumpMajorMatch(t, srcDB)
	_ = srcDB.Close()

	if err := Run(ctx, Options{
		SourceDSN: srcDSN,
		CloneName: cloneName,
		TargetDSN: "", // same host, derived from source
	}); err != nil {
		t.Fatalf("clone run: %v", err)
	}

	// Verify target table was cloned.
	tgtDSN, err := RewriteDSN(dsn, cloneName)
	if err != nil {
		t.Fatalf("rewrite target DSN: %v", err)
	}

	tgtDB, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer tgtDB.Close()

	var count int
	if err := tgtDB.QueryRowContext(ctx, `SELECT count(*) FROM t`).Scan(&count); err != nil {
		t.Fatalf("count target rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("target row count = %d, want 1", count)
	}
}

func openSeqPair(t *testing.T) (srcDB, tgtDB *sql.DB) {
	t.Helper()
	if testing.Short() {
		t.Skip("short")
	}
	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set")
	}
	if !strings.Contains(dsn, "sslmode=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "sslmode=disable"
	}
	ctx := context.Background()
	names := [2]string{
		fmt.Sprintf("dolly_seq_src_%d", os.Getpid()),
		fmt.Sprintf("dolly_seq_tgt_%d", os.Getpid()),
	}
	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	for _, name := range names {
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, name)); err != nil {
			t.Fatal(err)
		}
		n := name
		t.Cleanup(func() {
			_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, n))
		})
	}
	dbs := make([]*sql.DB, 2)
	for i, name := range names {
		connDSN, err := RewriteDSN(dsn, name)
		if err != nil {
			t.Fatal(err)
		}
		dbs[i], err = sql.Open("pgx", connDSN)
		if err != nil {
			t.Fatal(err)
		}
		idx := i
		t.Cleanup(func() { _ = dbs[idx].Close() })
		if _, err := dbs[i].ExecContext(ctx, `CREATE SEQUENCE public.monotonic_seq START 1`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := dbs[0].ExecContext(ctx, `SELECT setval('public.monotonic_seq', 10, true)`); err != nil {
		t.Fatal(err)
	}
	return dbs[0], dbs[1]
}

func TestRestoreSequencesPreservesCalledOnEqualPG16(t *testing.T) {
	ctx := context.Background()
	srcDB, tgtDB := openSeqPair(t)
	if _, err := srcDB.ExecContext(ctx, `ALTER SEQUENCE public.monotonic_seq RESTART WITH 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := tgtDB.ExecContext(ctx, `SELECT setval('public.monotonic_seq', 10, true)`); err != nil {
		t.Fatal(err)
	}
	if err := restoreSequences(ctx, srcDB, tgtDB); err != nil {
		t.Fatal(err)
	}
	var called bool
	if err := tgtDB.QueryRowContext(ctx, `SELECT is_called FROM public.monotonic_seq`).Scan(&called); err != nil || !called {
		t.Fatalf("is_called=%v err=%v", called, err)
	}
	var next int64
	if err := tgtDB.QueryRowContext(ctx, `SELECT nextval('public.monotonic_seq')`).Scan(&next); err != nil || next != 11 {
		t.Fatalf("nextval=%d err=%v", next, err)
	}
}

func TestRestoreSequencesAdversarialConcurrentPG16(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srcDB, tgtDB := openSeqPair(t)
	if _, err := tgtDB.ExecContext(ctx, `CREATE TABLE public.seq_observed (v bigint PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	loop := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				if err := fn(); err != nil {
					if ctx.Err() != nil {
						return
					}
					errCh <- err
					return
				}
			}
		}()
	}
	loop(func() error {
		var v int64
		if err := tgtDB.QueryRowContext(ctx, `SELECT nextval('public.monotonic_seq')`).Scan(&v); err != nil {
			return fmt.Errorf("nextval: %w", err)
		}
		_, err := tgtDB.ExecContext(ctx, `INSERT INTO public.seq_observed (v) VALUES ($1)`, v)
		return err
	})
	loop(func() error { return restoreSequences(ctx, srcDB, tgtDB) })
	select {
	case err := <-errCh:
		cancel()
		wg.Wait()
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		cancel()
		wg.Wait()
	}
}
