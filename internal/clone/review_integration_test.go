//go:build integration

package clone

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
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

func TestCatalogReplayReplicaStorageAndConstraintCommentsPG16(t *testing.T) {
	ctx := context.Background()
	src, tgt, _, _ := reviewDBPair(t)
	previous := db.SkipRelationAnnotations
	db.SkipRelationAnnotations = false
	t.Cleanup(func() { db.SkipRelationAnnotations = previous })
	if _, err := src.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE DOMAIN app.label AS text CONSTRAINT label_check CHECK (length(VALUE) > 0);
		COMMENT ON CONSTRAINT label_check ON DOMAIN app.label IS 'not empty';
		CREATE TABLE app.items (id integer NOT NULL, label app.label);
		CREATE UNIQUE INDEX items_id_idx ON app.items (id);
		ALTER TABLE app.items REPLICA IDENTITY USING INDEX items_id_idx;
		CREATE TABLE app.pk_items (id integer, CONSTRAINT custom_pk PRIMARY KEY (id));
		COMMENT ON CONSTRAINT custom_pk ON app.pk_items IS 'renamed pk';
		CREATE TABLE app.z_parent (id integer, body text) PARTITION BY RANGE (id);
		CREATE TABLE app.a_leaf PARTITION OF app.z_parent FOR VALUES FROM (0) TO (10);
		CREATE TABLE app.b_leaf PARTITION OF app.z_parent FOR VALUES FROM (10) TO (20);
		ALTER TABLE ONLY app.z_parent ALTER COLUMN body SET STORAGE EXTERNAL;
		ALTER TABLE ONLY app.b_leaf ALTER COLUMN body SET STORAGE MAIN;
	`); err != nil {
		t.Fatal(err)
	}
	if err := applySchemas(ctx, src, tgt, []string{"app"}, false); err != nil {
		t.Fatal(err)
	}
	var ident, index string
	if err := tgt.QueryRowContext(ctx, `SELECT c.relreplident::text, i.relname FROM pg_class c JOIN pg_index x ON x.indrelid = c.oid AND x.indisreplident JOIN pg_class i ON i.oid = x.indexrelid WHERE c.oid = 'app.items'::regclass`).Scan(&ident, &index); err != nil || ident != "i" || index != "items_id_idx" {
		t.Fatalf("replica identity = %q on %q, err = %v", ident, index, err)
	}
	for _, table := range []struct{ name, storage string }{{"a_leaf", "x"}, {"b_leaf", "m"}, {"z_parent", "e"}} {
		var got string
		if err := tgt.QueryRowContext(ctx, `SELECT a.attstorage::text FROM pg_attribute a WHERE a.attrelid = $1::regclass AND a.attname = 'body'`, "app."+table.name).Scan(&got); err != nil || got != table.storage {
			t.Fatalf("%s storage = %q, err = %v, want %q", table.name, got, err, table.storage)
		}
	}
	var comment string
	if err := tgt.QueryRowContext(ctx, `SELECT obj_description(c.oid, 'pg_constraint') FROM pg_constraint c WHERE c.contypid = 'app.label'::regtype AND c.conname = 'label_check'`).Scan(&comment); err != nil || comment != "not empty" {
		t.Fatalf("domain constraint comment = %q, err = %v", comment, err)
	}
}

func TestCatalogReplayIdentityTypesAndPartitionIndexesPG16(t *testing.T) {
	ctx := context.Background()
	src, tgt, _, _ := reviewDBPair(t)
	previous := db.SkipRelationAnnotations
	db.SkipRelationAnnotations = false
	t.Cleanup(func() { db.SkipRelationAnnotations = previous })
	if _, err := src.ExecContext(ctx, `
		CREATE SCHEMA app;
		CREATE TYPE app.mood AS ENUM ('happy', 'sad');
		CREATE TABLE app.items (id bigint GENERATED ALWAYS AS IDENTITY (SEQUENCE NAME app.items_custom_seq INCREMENT BY 2 START WITH 7), m app.mood, moods app.mood[]);
		INSERT INTO app.items (m) VALUES ('happy'), ('sad');
		CREATE TABLE app.z_parent (id integer, m app.mood) PARTITION BY RANGE (id);
		CREATE TABLE app.a_leaf PARTITION OF app.z_parent FOR VALUES FROM (0) TO (10);
		CREATE INDEX z_parent_m_idx ON app.z_parent (m);
		CREATE INDEX a_local_m_idx ON app.a_leaf (m);
	`); err != nil {
		t.Fatal(err)
	}
	// Force format_type to abbreviate app.mood on the source connection.
	src.SetMaxOpenConns(1)
	if _, err := src.ExecContext(ctx, `SET search_path TO app, public`); err != nil {
		t.Fatal(err)
	}
	if err := applySchemas(ctx, src, tgt, []string{"app"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := tgt.ExecContext(ctx, `INSERT INTO app.items (id, m) OVERRIDING SYSTEM VALUE VALUES (7, 'happy'), (9, 'sad')`); err != nil {
		t.Fatal(err)
	}
	if err := restoreSequences(ctx, src, tgt, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := tgt.QueryRowContext(ctx, `INSERT INTO app.items (m) VALUES ('happy') RETURNING id`).Scan(&id); err != nil || id != 11 {
		t.Fatalf("next identity = %d, err = %v, want 11", id, err)
	}
	var local, inherited int
	if err := tgt.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE idx.relname = 'a_local_m_idx' AND inh.inhrelid IS NULL),
		       count(*) FILTER (WHERE inh.inhrelid IS NOT NULL)
		FROM pg_class idx
		JOIN pg_index i ON i.indexrelid = idx.oid
		JOIN pg_class tbl ON tbl.oid = i.indrelid
		LEFT JOIN pg_inherits inh ON inh.inhrelid = idx.oid
		WHERE tbl.oid = 'app.a_leaf'::regclass
	`).Scan(&local, &inherited); err != nil || local != 1 || inherited != 1 {
		t.Fatalf("leaf indexes: local = %d, inherited = %d, err = %v", local, inherited, err)
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
