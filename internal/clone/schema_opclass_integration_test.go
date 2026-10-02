//go:build integration

package clone

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
)

func TestCatalogReplaySharedOperatorFamily(t *testing.T) {
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
	defer admin.Close()
	srcName := fmt.Sprintf("dolly_opclass_src_%d", os.Getpid())
	tgtName := fmt.Sprintf("dolly_opclass_tgt_%d", os.Getpid())
	for _, name := range []string{srcName, tgtName} {
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)); err != nil {
			t.Fatal(err)
		}
		name := name
		t.Cleanup(func() {
			_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name))
		})
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, name)); err != nil {
			t.Fatal(err)
		}
	}
	srcDSN, err := RewriteDSN(dsn, srcName)
	if err != nil {
		t.Fatal(err)
	}
	tgtDSN, err := RewriteDSN(dsn, tgtName)
	if err != nil {
		t.Fatal(err)
	}
	src, err := sql.Open("pgx", srcDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	tgt, err := sql.Open("pgx", tgtDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })
	if _, err := src.ExecContext(ctx, `
		CREATE OPERATOR FAMILY public.shared USING btree;
		CREATE OPERATOR CLASS public.a FOR TYPE integer USING btree
		  FAMILY public.shared AS
		  OPERATOR 1 < (integer, integer),
		  FUNCTION 1 btint4cmp(integer, integer);
		CREATE OPERATOR CLASS public.b FOR TYPE integer USING btree
		  FAMILY public.shared AS OPERATOR 3 = (integer, integer);
		CREATE TABLE public.shared_values (v integer);
		CREATE INDEX shared_a_idx ON public.shared_values (v public.a);
		CREATE INDEX shared_b_idx ON public.shared_values (v public.b);
	`); err != nil {
		t.Fatal(err)
	}
	if err := ApplySchemasFromSource(ctx, src, tgt, []string{"public"}); err != nil {
		t.Fatalf("catalog replay: %v", err)
	}
	var indexes int
	if err := tgt.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'i'
		  AND c.relname IN ('shared_a_idx', 'shared_b_idx')`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 2 {
		t.Fatalf("replayed indexes = %d, want 2", indexes)
	}
}
