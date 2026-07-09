//go:build integration

package restore

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

var integrationDB *sql.DB

func TestMain(m *testing.M) {
	db, err := pgintegration.SetupMainDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres integration setup: %v\n", err)
		os.Exit(1)
	}
	integrationDB = db
	code := m.Run()
	if integrationDB != nil {
		_ = integrationDB.Close()
	}
	os.Exit(code)
}

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	if integrationDB == nil {
		conn := pgintegration.Open(t)
		pgintegration.ApplyFixtures(t, conn)
		return conn
	}
	pgintegration.ApplyFixtures(t, integrationDB)
	return integrationDB
}

func TestIntegrationDumpRestoreRoundTrip(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	truncateFixtureData(t, conn, ctx)

	if err := Restore(ctx, conn, dir); err != nil {
		t.Fatalf("restore with default error policy: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("tbl_a count = %d, want 8", count)
	}

	var name string
	if err := conn.QueryRowContext(ctx, `SELECT name FROM tbl_a WHERE id = 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Alice Chen" {
		t.Fatalf("name = %q, want Alice Chen", name)
	}
}

func truncateFixtureData(t *testing.T, conn *sql.DB, ctx context.Context) {
	t.Helper()
	_, err := conn.ExecContext(ctx, `
		TRUNCATE project_members, projects, tbl_a, departments, empty_audit
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate fixture data: %v", err)
	}
}

func integrationDump(t *testing.T, conn *sql.DB) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	if err := dump.Dump(ctx, conn, dir); err != nil {
		t.Fatalf("dump: %v", err)
	}
	return dir
}

func TestIntegrationRestoreSchemaMismatchPreInsert(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, tbl := range meta.Tables {
		if tbl.Name != "tbl_a" {
			continue
		}
		for j, col := range tbl.Columns {
			if col.Name == "id" {
				meta.Tables[i].Columns[j].DataType = "text"
				break
			}
		}
		break
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = Restore(ctx, conn, dir, WithReplace())
	if err == nil {
		t.Fatal("expected schema validation error before insert")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("error = %v, want type mismatch", err)
	}
}

func TestIntegrationRestoreConflictSkip(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	if err := Restore(ctx, conn, dir, WithReplace()); err != nil {
		t.Fatalf("first restore: %v", err)
	}

	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	if err := Restore(ctx, conn, dir, WithConflictPolicy(ConflictSkip)); err != nil {
		t.Fatalf("skip restore: %v", err)
	}

	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("tbl_a count after skip = %d, want %d", after, before)
	}
}

func TestIntegrationRestoreConflictUpsert(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	if err := Restore(ctx, conn, dir, WithReplace()); err != nil {
		t.Fatalf("initial restore: %v", err)
	}

	const wantName = "n1 (upserted)"
	if err := patchNDJSONField(filepath.Join(dir, "tbl_a.ndjson"), "id", float64(1), "name", wantName); err != nil {
		t.Fatal(err)
	}

	if err := Restore(ctx, conn, dir, WithConflictPolicy(ConflictUpsert)); err != nil {
		t.Fatalf("upsert restore: %v", err)
	}

	var name string
	if err := conn.QueryRowContext(ctx, `SELECT name FROM tbl_a WHERE id = 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != wantName {
		t.Fatalf("name = %q, want %q", name, wantName)
	}
}

func TestIntegrationRestoreConflictErrorOnDuplicate(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	if err := Restore(ctx, conn, dir, WithReplace()); err != nil {
		t.Fatalf("initial restore: %v", err)
	}

	err := Restore(ctx, conn, dir)
	if err == nil {
		t.Fatal("expected duplicate key error with default error policy")
	}
}

func TestIntegrationRestoreWithoutTransaction(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	truncateFixtureData(t, conn, ctx)

	if err := Restore(ctx, conn, dir, WithoutTransaction()); err != nil {
		t.Fatalf("restore without transaction: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatalf("departments count = %d, want 4", count)
	}
}

func TestIntegrationRestoreExtraTargetColumn(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	const col = "dolly_restore_verify_extra"
	_, err := conn.ExecContext(ctx, `ALTER TABLE tbl_a ADD COLUMN `+col+` TEXT`)
	if err != nil {
		t.Fatalf("add extra column: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `ALTER TABLE tbl_a DROP COLUMN IF EXISTS `+col)
	})

	truncateFixtureData(t, conn, ctx)

	if err := Restore(ctx, conn, dir); err != nil {
		t.Fatalf("restore with extra target column: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("tbl_a count = %d, want 8", count)
	}
}

func TestIntegrationRestoreReplaceReloadsData(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	if err := Restore(ctx, conn, dir, WithReplace()); err != nil {
		t.Fatalf("initial restore: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `DELETE FROM tbl_a WHERE id = 1`); err != nil {
		t.Fatal(err)
	}

	var afterDelete int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&afterDelete); err != nil {
		t.Fatal(err)
	}
	if afterDelete == 0 {
		t.Fatal("expected some tbl_a remaining after delete")
	}

	if err := Restore(ctx, conn, dir, WithReplace()); err != nil {
		t.Fatalf("replace restore: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("tbl_a count = %d, want 8 from fixture dump", count)
	}

	var name string
	if err := conn.QueryRowContext(ctx, `SELECT name FROM tbl_a WHERE id = 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Alice Chen" {
		t.Fatalf("restored name = %q, want Alice Chen", name)
	}
}

func patchNDJSONField(path, keyField string, keyValue any, updateField, updateValue string) error {
	lines, err := readNDJSONLines(path)
	if err != nil {
		return err
	}

	var out []byte
	updated := false
	for _, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return err
		}
		if row[keyField] == keyValue {
			row[updateField] = updateValue
			updated = true
		}
		data, err := json.Marshal(row)
		if err != nil {
			return err
		}
		out = append(out, data...)
		out = append(out, '\n')
	}

	if !updated {
		return os.ErrNotExist
	}
	return os.WriteFile(path, out, 0o644)
}

func TestIntegrationRestoreSubsetDump(t *testing.T) {
	conn := openIntegrationDB(t)
	dumpDir := t.TempDir()
	ctx := context.Background()

	cfg := dump.SubsetConfig{
		Seeds: []dump.RowPredicate{
			{Table: "tbl_a", Column: "id", Op: dump.PredicateIn, Values: []any{int64(1)}},
		},
		Limits: dump.DefaultSubsetLimits(),
	}
	if err := dump.Dump(ctx, conn, dumpDir, dump.WithSubset(cfg)); err != nil {
		t.Fatal(err)
	}

	truncateFixtureData(t, conn, ctx)

	if err := Restore(ctx, conn, dumpDir); err != nil {
		t.Fatalf("restore subset dump: %v", err)
	}

	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatalf("restored tbl_a count = %d, want at least 1", count)
	}
}

func readNDJSONLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}
