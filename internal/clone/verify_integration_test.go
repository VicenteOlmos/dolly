//go:build integration

package clone

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestSchemaReplayVerifyUsageOnlySequence(t *testing.T) {
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
	pid := os.Getpid()
	role := fmt.Sprintf("dolly_verify_role_%d", pid)
	rolePass := fmt.Sprintf("verify_%d_pw", pid)
	srcName := fmt.Sprintf("dolly_verify_src_%d", pid)
	tgtName := fmt.Sprintf("dolly_verify_tgt_%d", pid)

	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, srcName))
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, tgtName))
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP ROLE IF EXISTS "%s"`, role))
	})
	_, _ = admin.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, srcName))
	_, _ = admin.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, tgtName))
	_, _ = admin.ExecContext(ctx, fmt.Sprintf(`DROP ROLE IF EXISTS "%s"`, role))
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s'`, role, rolePass)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{srcName, tgtName} {
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`GRANT CONNECT, CREATE ON DATABASE "%s" TO "%s"`, name, role)); err != nil {
			t.Fatal(err)
		}
	}
	srcAdminDSN, err := RewriteDSN(dsn, srcName)
	if err != nil {
		t.Fatal(err)
	}
	srcAdmin, err := sql.Open("pgx", srcAdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer srcAdmin.Close()
	if _, err := srcAdmin.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE TABLE app.orders (id serial PRIMARY KEY, n int);
		INSERT INTO app.orders (n) VALUES (1);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := srcAdmin.ExecContext(ctx, fmt.Sprintf(`
		REVOKE ALL ON SEQUENCE app.orders_id_seq FROM PUBLIC;
		GRANT USAGE ON SCHEMA app TO "%s";
		GRANT SELECT ON app.orders TO "%s";
		GRANT USAGE ON SEQUENCE app.orders_id_seq TO "%s";
	`, role, role, role)); err != nil {
		t.Fatal(err)
	}

	srcDSN, err := rewriteDSNWithUser(dsn, role, rolePass, srcName)
	if err != nil {
		t.Fatal(err)
	}
	tgtDSN, err := rewriteDSNWithUser(dsn, role, rolePass, tgtName)
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	if err := Run(ctx, Options{
		SourceDSN:      srcDSN,
		CloneName:      tgtName,
		TargetDSN:      tgtDSN,
		SkipCreate:     true,
		Strategy:       "schema-replay",
		Verify:         true,
		VerifyWarnings: &warnings,
		DumpOpts:       []dump.Option{dump.WithSchemas([]string{"app"})},
	}); err != nil {
		t.Fatalf("clone: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "orders_id_seq") || !strings.Contains(joined, "unavailable") {
		t.Fatalf("warnings = %#v, want an unavailable sequence warning", warnings)
	}
}
