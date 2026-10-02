package clone

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestSchemaReplayFallsBackWhenPgDumpMissing(t *testing.T) {
	origLook := schemaToolLookPath
	schemaToolLookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { schemaToolLookPath = origLook })

	var gotSchemas []string
	var gotPriv bool
	var calls int
	var clusterCalls int
	prevCluster := applyClusterGlobalsDB
	applyClusterGlobalsDB = func(context.Context, *sql.DB, *sql.DB) error {
		clusterCalls++
		return nil
	}
	t.Cleanup(func() { applyClusterGlobalsDB = prevCluster })
	origApply := applySchemasFromSourceFn
	applySchemasFromSourceFn = func(_ context.Context, src, tgt *sql.DB, schemas []string, includePrivileges bool) error {
		calls++
		if src == nil || tgt == nil {
			t.Fatal("catalog replay needs source and target databases")
		}
		gotSchemas = append([]string(nil), schemas...)
		gotPriv = includePrivileges
		return nil
	}
	t.Cleanup(func() { applySchemasFromSourceFn = origApply })

	origOpen := sqlOpenDB
	sqlOpenDB = func(string) (*sql.DB, error) {
		return sql.Open("pgx", "postgres://u:p@h-a:5432/db_src")
	}
	t.Cleanup(func() { sqlOpenDB = origOpen })

	origDump := dumpFunc
	dumpFunc = func(context.Context, *sql.DB, string, ...dump.Option) error { return nil }
	t.Cleanup(func() { dumpFunc = origDump })

	origRestore := restoreFunc
	restoreFunc = func(context.Context, *sql.DB, string, ...restore.Option) error { return nil }
	t.Cleanup(func() { restoreFunc = origRestore })

	runner := &mockCommandRunner{}
	strat := &SchemaReplayStrategy{Runner: runner}
	err := strat.Execute(context.Background(), Options{
		SourceDSN:         "postgres://u:p@h-a:5432/db_src",
		CloneName:         "db_clone",
		SkipCreate:        true,
		IncludePrivileges: true,
		DumpOpts:          []dump.Option{dump.WithSchemas([]string{"public"})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.pipeCalls) != 0 {
		t.Fatalf("pipe calls = %d, want catalog replay without pg_dump", len(runner.pipeCalls))
	}
	if calls != 1 {
		t.Fatalf("catalog replay calls = %d, want 1", calls)
	}
	if clusterCalls != 1 {
		t.Fatalf("cluster object calls = %d, want 1", clusterCalls)
	}
	if !gotPriv {
		t.Fatal("catalog replay should keep IncludePrivileges")
	}
	if len(gotSchemas) != 1 || gotSchemas[0] != "public" {
		t.Fatalf("schemas = %v, want [public]", gotSchemas)
	}
}
