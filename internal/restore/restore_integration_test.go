//go:build integration

package restore

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

var (
	integrationDB        *sql.DB
	restoreIntegrationMu sync.Mutex
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}
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
	restoreIntegrationMu.Lock()
	t.Cleanup(func() {
		if integrationDB != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := pgintegration.Bootstrap(ctx, integrationDB); err != nil {
				fmt.Fprintf(os.Stderr, "restore integration: reset fixtures: %v\n", err)
			}
			waitIntegrationDBIdle(integrationDB, 2*time.Second)
		}
		restoreIntegrationMu.Unlock()
	})
	if integrationDB == nil {
		conn := pgintegration.Open(t)
		pgintegration.ApplyFixtures(t, conn)
		return conn
	}
	pgintegration.ApplyFixtures(t, integrationDB)
	return integrationDB
}

func waitIntegrationDBIdle(conn *sql.DB, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn.Stats().InUse == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestIntegrationRestoreSelectedSchemasSlowChunkDump(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	schemas := []string{"app", "public"}

	if err := dump.Dump(ctx, conn, dir,
		dump.WithSchemas(schemas),
		dump.WithSlowConnection(),
		dump.WithChunkTables([]dump.QualifiedTable{{Schema: "public", Name: "tbl_a"}}),
		dump.WithProvenance(dump.Provenance{Schemas: append([]string(nil), schemas...)}),
	); err != nil {
		t.Fatalf("dump: %v", err)
	}

	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Provenance == nil || len(meta.Provenance.Schemas) != 2 {
		t.Fatalf("provenance schemas = %v, want %v", meta.Provenance, schemas)
	}
	if len(meta.Sequences) == 0 {
		t.Fatal("expected sequences in multi-schema slow/chunk dump")
	}
	for _, seq := range meta.Sequences {
		if seq.Schema != "app" && seq.Schema != "public" {
			t.Fatalf("sequence %s.%s outside selected schemas", seq.Schema, seq.Name)
		}
	}

	truncateFixtureData(t, conn, ctx)
	if err := Restore(ctx, conn, dir); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var invoiceCount, tblACount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM app.invoices`).Scan(&invoiceCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&tblACount); err != nil {
		t.Fatal(err)
	}
	if invoiceCount != 1 || tblACount != 8 {
		t.Fatalf("after restore invoices=%d tbl_a=%d, want 1 and 8", invoiceCount, tblACount)
	}

	assertRestoredSequenceState(t, conn, ctx, "app", "invoices_id_seq", 1, true)
	assertRestoredSequenceState(t, conn, ctx, "public", "tbl_a_id_seq", 8, true)

	var nextInvoiceID int
	if err := conn.QueryRowContext(ctx, `INSERT INTO app.invoices (note) VALUES ('post-restore seq') RETURNING id`).Scan(&nextInvoiceID); err != nil {
		t.Fatal(err)
	}
	if nextInvoiceID != 2 {
		t.Fatalf("next app.invoices id = %d, want 2 after sequence restore", nextInvoiceID)
	}
}

func assertRestoredSequenceState(t *testing.T, conn *sql.DB, ctx context.Context, schema, seqName string, wantLast int, wantCalled bool) {
	t.Helper()
	qualified := quoteIdentifier(schema) + "." + quoteIdentifier(seqName)
	var last int
	var called bool
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT last_value, is_called FROM %s`, qualified)).Scan(&last, &called); err != nil {
		t.Fatalf("read sequence %s.%s: %v", schema, seqName, err)
	}
	if last != wantLast || called != wantCalled {
		t.Fatalf("sequence %s.%s = (%d, called=%t), want (%d, called=%t)", schema, seqName, last, called, wantLast, wantCalled)
	}
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
		TRUNCATE app.invoices, project_members, projects, tbl_a, departments, empty_audit
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

func TestIntegrationRestoreRejectsEmptyDump(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Schema:      "public",
		Tables:      nil,
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	err = Restore(ctx, conn, dir)
	if !IsEmptyDumpError(err) {
		t.Fatalf("error = %v, want EmptyDumpError", err)
	}

	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("tbl_a count changed %d -> %d after empty restore refusal", before, after)
	}
}

func TestIntegrationRestoreRejectsNoDataFiles(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	meta := dump.Metadata{
		GeneratedAt: "2026-01-01T00:00:00Z",
		Schema:      "public",
		Tables: []db.Table{
			{Name: "tbl_a", Columns: []db.Column{{Name: "id", DataType: "integer"}}},
			{Name: "departments", Columns: []db.Column{{Name: "id", DataType: "integer"}}},
		},
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	err = Restore(ctx, conn, dir)
	if !IsNoDataFilesError(err) {
		t.Fatalf("error = %v, want NoDataFilesError", err)
	}

	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("tbl_a count changed %d -> %d after no-data-files restore refusal", before, after)
	}
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
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	var path string
	for _, table := range meta.Tables {
		if table.Name == "tbl_a" {
			path, err = resolveDataFile(dir, table)
			break
		}
	}
	if err != nil || path == "" {
		t.Fatalf("resolve tbl_a data file: %v", err)
	}
	if err := patchNDJSONField(path, "id", float64(1), "name", wantName); err != nil {
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

func TestIntegrationLoadTableCopy(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()

	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	truncateFixtureData(t, conn, ctx)

	for _, table := range meta.Tables {
		if table.Name != "departments" {
			continue
		}
		path, err := resolveDataFile(dir, table)
		if err != nil {
			t.Fatal(err)
		}
		if err := loadTableCopy(ctx, os.Getenv(pgintegration.EnvDSN), table, path); err != nil {
			t.Fatal(err)
		}

		var count int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 4 {
			t.Fatalf("departments count = %d, want 4", count)
		}
		return
	}
	t.Fatal("departments missing from dump metadata")
}

func TestIntegrationRestoreReplaceRejectsExternalForeignKey(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()
	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.ExecContext(ctx, `CREATE TABLE dolly_restore_external_fk (id integer PRIMARY KEY, parent_id integer REFERENCES tbl_a(id))`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), `DROP TABLE dolly_restore_external_fk`) })

	if err := Restore(ctx, conn, dir, WithReplace()); err == nil {
		t.Fatal("expected external foreign key to reject replace")
	}
	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("tbl_a changed after rejected replace: %d -> %d", before, after)
	}
}

func TestIntegrationRestoreReplaceRollsBackAfterLaterLoadFailure(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := integrationDump(t, conn)
	ctx := context.Background()
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) < 2 {
		t.Fatal("fixture needs multiple tables")
	}
	path, err := resolveDataFile(dir, meta.Tables[len(meta.Tables)-1])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Restore(ctx, conn, dir, WithReplace()); err == nil {
		t.Fatal("expected later table load failure")
	}
	var after int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM tbl_a`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("replace rollback lost tbl_a rows: %d -> %d", before, after)
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

const parallelRestoreParentSeqInjectedLast = int64(9001)

func setupParallelRestoreTables(t *testing.T, conn *sql.DB, ctx context.Context) {
	t.Helper()
	_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS dolly_par_restore_parent (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS dolly_par_restore_child (
			id SERIAL PRIMARY KEY,
			parent_id INTEGER NOT NULL REFERENCES dolly_par_restore_parent(id),
			label TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS dolly_par_restore_grandchild (
			id SERIAL PRIMARY KEY,
			child_id INTEGER NOT NULL REFERENCES dolly_par_restore_child(id),
			note TEXT NOT NULL
		);
		TRUNCATE dolly_par_restore_grandchild, dolly_par_restore_child, dolly_par_restore_parent RESTART IDENTITY CASCADE;
		INSERT INTO dolly_par_restore_parent (id, name) VALUES (1, 'alpha'), (2, 'beta');
		INSERT INTO dolly_par_restore_child (id, parent_id, label) VALUES (1, 1, 'one'), (2, 2, 'two');
		INSERT INTO dolly_par_restore_grandchild (id, child_id, note) VALUES (1, 1, 'gc-one'), (2, 2, 'gc-two');
	`)
	if err != nil {
		t.Fatalf("setup parallel restore tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS dolly_par_restore_grandchild, dolly_par_restore_child, dolly_par_restore_parent`)
	})
}

func parallelRestoreDump(t *testing.T, conn *sql.DB) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	policy := dump.SelectionPolicy{
		Includes: []dump.SelectorEntry{
			{Table: dump.QualifiedTable{Schema: "public", Name: "dolly_par_restore_parent"}},
			{Table: dump.QualifiedTable{Schema: "public", Name: "dolly_par_restore_child"}},
			{Table: dump.QualifiedTable{Schema: "public", Name: "dolly_par_restore_grandchild"}},
		},
	}
	if err := dump.Dump(ctx, conn, dir, dump.WithTableSelection(policy, nil), dump.WithoutSequences()); err != nil {
		t.Fatalf("dump: %v", err)
	}
	return dir
}

func corruptParallelRestoreChildData(t *testing.T, dir string) {
	t.Helper()
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range meta.Tables {
		if table.Name != "dolly_par_restore_child" {
			continue
		}
		childPath, err := resolveDataFile(dir, table)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(childPath, []byte("not-json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("child data file missing")
}

func truncateParallelRestoreTables(t *testing.T, conn *sql.DB, ctx context.Context) {
	t.Helper()
	_, err := conn.ExecContext(ctx, `TRUNCATE dolly_par_restore_grandchild, dolly_par_restore_child, dolly_par_restore_parent RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func assertParallelParentSequenceBaseline(t *testing.T, conn *sql.DB, ctx context.Context) {
	t.Helper()
	var seqLast int
	var seqCalled bool
	if err := conn.QueryRowContext(ctx, `SELECT last_value, is_called FROM dolly_par_restore_parent_id_seq`).Scan(&seqLast, &seqCalled); err != nil {
		t.Fatal(err)
	}
	if seqLast != 1 || seqCalled {
		t.Fatalf("parent sequence = (%d, called=%t), want (1, false) post-TRUNCATE baseline; sequence phase must not run", seqLast, seqCalled)
	}
	if seqLast == int(parallelRestoreParentSeqInjectedLast) {
		t.Fatalf("parent sequence at injected metadata value %d; sequence phase ran", parallelRestoreParentSeqInjectedLast)
	}
}

func parallelRestoreOpts(dsn, manifestPath string, workers int, schemas ...string) []Option {
	opts := []Option{
		WithWorkers(workers),
		WithoutTransaction(),
		WithDSN(dsn),
		WithPartialStateManifest(manifestPath),
	}
	if len(schemas) > 0 {
		opts = append(opts, WithSchemas(schemas))
	}
	return opts
}

func injectParallelRestoreParentSequenceMetadata(t *testing.T, dir string) {
	t.Helper()
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	last := parallelRestoreParentSeqInjectedLast
	meta.Sequences = []dump.SequenceState{{
		Schema:     "public",
		Name:       "dolly_par_restore_parent_id_seq",
		LastValue:  &last,
		StartValue: 1,
		IsCalled:   true,
	}}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationParallelRestoreSuccess(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	setupParallelRestoreTables(t, conn, ctx)

	dir := parallelRestoreDump(t, conn)
	injectParallelRestoreParentSequenceMetadata(t, dir)
	_, err := conn.ExecContext(ctx, `TRUNCATE dolly_par_restore_grandchild, dolly_par_restore_child, dolly_par_restore_parent RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(dir, "parallel-state.json")
	dsn := os.Getenv(pgintegration.EnvDSN)
	if err := Restore(ctx, conn, dir, parallelRestoreOpts(dsn, manifestPath, 2, "public")...); err != nil {
		t.Fatalf("parallel restore: %v", err)
	}
	waitIntegrationDBIdle(conn, 2*time.Second)
	if _, err := os.Stat(manifestPath); err == nil {
		t.Fatalf("manifest %q should be removed after success", manifestPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat manifest: %v", err)
	}

	var parentCount, childCount, grandchildCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_parent`).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_child`).Scan(&childCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_grandchild`).Scan(&grandchildCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 2 || childCount != 2 || grandchildCount != 2 {
		t.Fatalf("counts parent=%d child=%d grandchild=%d, want 2/2/2", parentCount, childCount, grandchildCount)
	}

	var seqLast int
	var seqCalled bool
	if err := conn.QueryRowContext(ctx, `SELECT last_value, is_called FROM dolly_par_restore_parent_id_seq`).Scan(&seqLast, &seqCalled); err != nil {
		t.Fatal(err)
	}
	if seqLast != 2 || !seqCalled {
		t.Fatalf("parent sequence = (%d, called=%t), want (2, true) after metadata restore then sync", seqLast, seqCalled)
	}
	if seqLast == int(parallelRestoreParentSeqInjectedLast) {
		t.Fatalf("parent sequence still at injected metadata value %d; sync to data did not run", parallelRestoreParentSeqInjectedLast)
	}
	var nextID int
	if err := conn.QueryRowContext(ctx, `INSERT INTO dolly_par_restore_parent (name) VALUES ('gamma') RETURNING id`).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if nextID != 3 {
		t.Fatalf("next parent id = %d, want 3 after sequence sync", nextID)
	}
}

func TestIntegrationParallelRestorePartialFailure(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	setupParallelRestoreTables(t, conn, ctx)

	dir := parallelRestoreDump(t, conn)
	injectParallelRestoreParentSequenceMetadata(t, dir)
	corruptParallelRestoreChildData(t, dir)
	truncateParallelRestoreTables(t, conn, ctx)

	manifestPath := filepath.Join(dir, "parallel-fail-state.json")
	dsn := os.Getenv(pgintegration.EnvDSN)
	err := Restore(ctx, conn, dir, parallelRestoreOpts(dsn, manifestPath, 2)...)
	waitIntegrationDBIdle(conn, 2*time.Second)
	if err == nil {
		t.Fatal("expected parallel restore failure")
	}

	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %o, want 0600", info.Mode().Perm())
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, secret := range []string{dsn, "password", "postgres://"} {
		if secret != "" && strings.Contains(raw, secret) {
			t.Fatalf("manifest leaks credential fragment %q", secret)
		}
	}

	manifest, err := LoadPartialStateManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Committed) != 1 || manifest.Committed[0] != "public.dolly_par_restore_parent" {
		t.Fatalf("committed = %v, want [public.dolly_par_restore_parent]", manifest.Committed)
	}
	if len(manifest.Failed) != 1 || manifest.Failed[0].Table != "public.dolly_par_restore_child" {
		t.Fatalf("failed = %+v, want child failure", manifest.Failed)
	}
	if len(manifest.Pending) != 1 || manifest.Pending[0] != "public.dolly_par_restore_grandchild" {
		t.Fatalf("pending = %v, want [public.dolly_par_restore_grandchild]", manifest.Pending)
	}

	var parentCount, childCount, grandchildCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_parent`).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_child`).Scan(&childCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_grandchild`).Scan(&grandchildCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 2 {
		t.Fatalf("parent count = %d, want 2 committed before child failure", parentCount)
	}
	if childCount != 0 || grandchildCount != 0 {
		t.Fatalf("child=%d grandchild=%d, want 0 after failed COPY", childCount, grandchildCount)
	}
	assertParallelParentSequenceBaseline(t, conn, ctx)
}

func TestIntegrationApplySchemaSQLSingleTransactionRollsBackOnError(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	dsn := os.Getenv(pgintegration.EnvDSN)
	dir := t.TempDir()

	const table = "dolly_trusted_schema_fail_test"
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS public.`+table)
	})

	schemaSQL := fmt.Sprintf(`CREATE TABLE public.%s (id integer);
SELECT * FROM dolly_nonexistent_trusted_schema_fail;`, table)
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte(schemaSQL), 0o600); err != nil {
		t.Fatal(err)
	}

	err := applySchemaSQL(ctx, dsn, dir)
	if err == nil {
		t.Fatal("expected schema.sql statement error")
	}

	var regclass *string
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass('public.`+table+`')`).Scan(&regclass); err != nil {
		t.Fatal(err)
	}
	if regclass != nil {
		t.Fatalf("table %q remained after failed replay, regclass=%v", table, *regclass)
	}
}

func TestIntegrationApplySchemaSQLSingleTransactionRollsBackOnCancel(t *testing.T) {
	conn := openIntegrationDB(t)
	dsn := os.Getenv(pgintegration.EnvDSN)
	dir := t.TempDir()

	const table = "dolly_trusted_schema_cancel_test"
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS public.`+table)
	})

	schemaSQL := fmt.Sprintf(`CREATE TABLE public.%s (id integer);
SELECT pg_sleep(120);`, table)
	if err := os.WriteFile(filepath.Join(dir, "schema.sql"), []byte(schemaSQL), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- applySchemaSQL(ctx, dsn, dir)
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected cancellation error from schema replay")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("applySchemaSQL did not return after context cancellation")
	}

	var regclass *string
	verifyCtx := context.Background()
	if err := conn.QueryRowContext(verifyCtx, `SELECT to_regclass('public.`+table+`')`).Scan(&regclass); err != nil {
		t.Fatal(err)
	}
	if regclass != nil {
		t.Fatalf("table %q remained after canceled replay, regclass=%v", table, *regclass)
	}
}

func TestIntegrationParallelRestoreRetryRetainsCommittedManifest(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	setupParallelRestoreTables(t, conn, ctx)

	dir := parallelRestoreDump(t, conn)
	injectParallelRestoreParentSequenceMetadata(t, dir)
	corruptParallelRestoreChildData(t, dir)
	truncateParallelRestoreTables(t, conn, ctx)

	manifestPath := filepath.Join(dir, "parallel-retry-state.json")
	if err := WritePartialStateManifest(manifestPath, PartialStateManifest{
		Committed: []string{"public.dolly_par_restore_parent"},
		Pending:   []string{"public.dolly_par_restore_child", "public.dolly_par_restore_grandchild"},
	}); err != nil {
		t.Fatal(err)
	}

	dsn := os.Getenv(pgintegration.EnvDSN)
	err := Restore(ctx, conn, dir, parallelRestoreOpts(dsn, manifestPath, 2, "public")...)
	waitIntegrationDBIdle(conn, 2*time.Second)
	if err == nil {
		t.Fatal("expected parallel restore retry failure")
	}

	manifest, err := LoadPartialStateManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Committed) != 1 || manifest.Committed[0] != "public.dolly_par_restore_parent" {
		t.Fatalf("committed = %v, want retained seeded parent", manifest.Committed)
	}
	if len(manifest.Failed) != 1 || manifest.Failed[0].Table != "public.dolly_par_restore_child" {
		t.Fatalf("failed = %+v, want child failure", manifest.Failed)
	}
	if len(manifest.Pending) != 1 || manifest.Pending[0] != "public.dolly_par_restore_grandchild" {
		t.Fatalf("pending = %v, want grandchild only", manifest.Pending)
	}

	var parentCount, childCount, grandchildCount int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_parent`).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_child`).Scan(&childCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dolly_par_restore_grandchild`).Scan(&grandchildCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 0 {
		t.Fatalf("parent count = %d, want 0 because seeded committed was not reloaded", parentCount)
	}
	if childCount != 0 || grandchildCount != 0 {
		t.Fatalf("child=%d grandchild=%d, want 0 after failed retry", childCount, grandchildCount)
	}
	assertParallelParentSequenceBaseline(t, conn, ctx)
}
