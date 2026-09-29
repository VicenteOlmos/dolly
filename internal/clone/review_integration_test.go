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

func reviewDBPair(t *testing.T) (*sql.DB, *sql.DB, string, string) {
	t.Helper()
	dsn := os.Getenv("DOLLY_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DOLLY_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	adminDSN, err := RewriteDSN(dsn, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	names := []string{fmt.Sprintf("dolly_review_src_%d", os.Getpid()), fmt.Sprintf("dolly_review_tgt_%d", os.Getpid())}
	for _, name := range names {
		if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoteIdentifier(name)); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(name)); err != nil {
			t.Fatal(err)
		}
		name := name
		t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdentifier(name)) })
	}
	connections := make([]*sql.DB, 2)
	dsns := make([]string, 2)
	for i, name := range names {
		dsns[i], err = RewriteDSN(dsn, name)
		if err != nil {
			t.Fatal(err)
		}
		connections[i], err = sql.Open("pgx", dsns[i])
		if err != nil {
			t.Fatal(err)
		}
		conn := connections[i]
		t.Cleanup(func() { conn.Close() })
	}
	return connections[0], connections[1], dsns[0], dsns[1]
}

func TestCatalogReplayFunctionDefaultsIndexesAndModesPG16(t *testing.T) {
	ctx := context.Background()
	src, tgt, _, _ := reviewDBPair(t)
	if _, err := src.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE FUNCTION app.make_id() RETURNS integer LANGUAGE sql IMMUTABLE AS $$ SELECT 7 $$;
		CREATE FUNCTION app.audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
		CREATE TABLE app.items (id integer DEFAULT app.make_id(), email text);
		CREATE TABLE app.rule_items (id integer);
		CREATE INDEX items_expr ON app.items ((app.make_id()));
		CREATE TRIGGER audit_disabled BEFORE INSERT ON app.items FOR EACH ROW EXECUTE FUNCTION app.audit();
		CREATE TRIGGER audit_replica BEFORE UPDATE ON app.items FOR EACH ROW EXECUTE FUNCTION app.audit();
		CREATE TRIGGER audit_always BEFORE DELETE ON app.items FOR EACH ROW EXECUTE FUNCTION app.audit();
		ALTER TABLE app.items DISABLE TRIGGER audit_disabled;
		ALTER TABLE app.items ENABLE REPLICA TRIGGER audit_replica;
		ALTER TABLE app.items ENABLE ALWAYS TRIGGER audit_always;
		CREATE RULE items_disabled AS ON DELETE TO app.rule_items DO INSTEAD NOTHING;
		CREATE RULE items_replica AS ON UPDATE TO app.rule_items DO INSTEAD NOTHING;
		CREATE RULE items_always AS ON INSERT TO app.rule_items DO INSTEAD NOTHING;
		ALTER TABLE app.rule_items DISABLE RULE items_disabled;
		ALTER TABLE app.rule_items ENABLE REPLICA RULE items_replica;
		ALTER TABLE app.rule_items ENABLE ALWAYS RULE items_always;
	`); err != nil {
		t.Fatal(err)
	}
	if err := applySchemas(ctx, src, tgt, []string{"app"}, true); err != nil {
		t.Fatal(err)
	}
	if err := applyTargetFidelity(ctx, src, tgt, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := tgt.ExecContext(ctx, `INSERT INTO app.items DEFAULT VALUES`); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := tgt.QueryRowContext(ctx, `SELECT id FROM app.items`).Scan(&id); err != nil || id != 7 {
		t.Fatalf("default function result = %d, err = %v", id, err)
	}
	var indexExists bool
	if err := tgt.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE schemaname = 'app' AND indexname = 'items_expr')`).Scan(&indexExists); err != nil || !indexExists {
		t.Fatalf("expression index exists = %v, err = %v", indexExists, err)
	}
	for _, query := range []string{
		`SELECT t.tgname, t.tgenabled FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'app' AND NOT t.tgisinternal ORDER BY t.tgname`,
		`SELECT r.rulename, r.ev_enabled FROM pg_rewrite r JOIN pg_class c ON c.oid = r.ev_class JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'app' AND r.rulename <> '_RETURN' ORDER BY r.rulename`,
	} {
		rows, err := tgt.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []struct{ name, mode string }{{"audit_always", "A"}, {"audit_disabled", "D"}, {"audit_replica", "R"}} {
			if strings.Contains(query, "pg_rewrite") {
				want.name = strings.Replace(want.name, "audit_", "items_", 1)
			}
			var name, mode string
			if !rows.Next() || rows.Scan(&name, &mode) != nil || name != want.name || mode != want.mode {
				t.Fatalf("mode for %s: got %s %s, want %s %s", query, name, mode, want.name, want.mode)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}

func TestCatalogReplayNormalAggregatePG16(t *testing.T) {
	ctx := context.Background()
	src, tgt, _, _ := reviewDBPair(t)
	if _, err := src.ExecContext(ctx, `CREATE SCHEMA app;
		CREATE FUNCTION app.sum_step(integer, integer) RETURNS integer LANGUAGE sql IMMUTABLE AS $$ SELECT $1 + $2 $$;
		CREATE AGGREGATE app.my_sum(integer) (SFUNC = app.sum_step, STYPE = integer, INITCOND = '0')`); err != nil {
		t.Fatal(err)
	}
	if err := applySchemas(ctx, src, tgt, []string{"app"}, false); err != nil {
		t.Fatal(err)
	}
	var got int
	if err := tgt.QueryRowContext(ctx, `SELECT app.my_sum(v) FROM (VALUES (1), (2)) AS s(v)`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("app.my_sum = %d, want 3", got)
	}
}

func TestSanitizedCopyEnumPG16(t *testing.T) {
	ctx := context.Background()
	src, tgt, srcDSN, tgtDSN := reviewDBPair(t)
	if _, err := src.ExecContext(ctx, `CREATE SCHEMA app; CREATE TYPE app.status AS ENUM ('active', 'inactive');
		CREATE TABLE app.users (id integer, email text, status app.status)`); err != nil {
		t.Fatal(err)
	}
	if _, err := src.ExecContext(ctx, `INSERT INTO app.users VALUES (1, 'person@example.com', 'active')`); err != nil {
		t.Fatal(err)
	}
	requirePgDumpMajorMatch(t, src)
	if err := (&CopyStreamStrategy{}).Execute(ctx, Options{
		SourceDSN: srcDSN, TargetDSN: tgtDSN, CloneName: "dolly_review_tgt_" + fmt.Sprint(os.Getpid()),
		SkipCreate: true, RowTransform: dump.SanitizeByPattern,
		DumpOpts: []dump.Option{dump.WithSchemas([]string{"app"})},
	}); err != nil {
		t.Fatal(err)
	}
	var email, status string
	if err := tgt.QueryRowContext(ctx, `SELECT email, status FROM app.users`).Scan(&email, &status); err != nil || email != "redacted@example.com" || status != "active" {
		t.Fatalf("email = %q, status = %q, err = %v", email, status, err)
	}
}
