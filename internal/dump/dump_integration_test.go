//go:build integration

package dump

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
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

func TestIntegrationDumpArtifacts(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"departments", "tbl_a", "projects", "project_members", "empty_audit"} {
		if _, err := os.Stat(tableDataPath(dir, metadataTable(t, meta, name))); err != nil {
			t.Fatalf("missing data for %s: %v", name, err)
		}
	}
}

func TestIntegrationDumpMetadataAndRows(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.GeneratedAt == "" {
		t.Fatal("missing generated_at")
	}
	if meta.Schema != "public" {
		t.Fatalf("schema = %q", meta.Schema)
	}

	names := map[string]bool{}
	for _, tbl := range meta.Tables {
		names[tbl.Name] = true
	}
	for _, want := range []string{"departments", "tbl_a", "projects", "project_members", "empty_audit"} {
		if !names[want] {
			t.Fatalf("metadata missing table %q", want)
		}
	}

	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "tbl_a")))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 tbl_a rows, got %d", len(lines))
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["id"]; !ok {
		t.Fatal("tbl_a row missing id key")
	}
	if _, ok := row["name"]; !ok {
		t.Fatal("tbl_a row missing name key")
	}
}

func TestIntegrationDumpEmptyTableFile(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "empty_audit")))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("empty_audit.ndjson has %d lines, want 0", len(lines))
	}
}

func TestIntegrationDumpDefaultSnapshot(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationDumpDependencyOrder(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	deptIdx, empIdx := -1, -1
	for i, tbl := range meta.Tables {
		switch tbl.Name {
		case "departments":
			deptIdx = i
		case "tbl_a":
			empIdx = i
		}
	}
	if deptIdx < 0 || empIdx < 0 {
		t.Fatal("missing departments or tbl_a in metadata order")
	}
	if deptIdx > empIdx {
		t.Fatalf("departments index %d after tbl_a %d", deptIdx, empIdx)
	}
}

func TestIntegrationDumpInvalidOutputPath(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Dump(ctx, conn, blocker)
	if err == nil {
		t.Fatal("expected error dumping to file path")
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

func metadataTable(t *testing.T, meta Metadata, name string) db.Table {
	t.Helper()
	for _, table := range meta.Tables {
		if table.Name == name {
			if table.DataFile == nil {
				t.Fatalf("metadata table %q missing data_file", name)
			}
			return table
		}
	}
	t.Fatalf("metadata missing table %q", name)
	return db.Table{}
}

func TestIntegrationSubsetDumpTableSeed(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)},
		},
		Limits: DefaultSubsetLimits(),
	}

	if err := Dump(ctx, conn, dir, WithSubset(cfg)); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Subset == nil {
		t.Fatal("expected subset manifest")
	}
	if len(meta.Subset.Tables) == 0 {
		t.Fatal("subset manifest missing tables")
	}

	tableSet := map[string]bool{}
	for _, name := range meta.Subset.Tables {
		tableSet[strings.TrimPrefix(name, "public\x00")] = true
	}
	for _, want := range []string{"departments", "tbl_a"} {
		if !tableSet[want] {
			t.Fatalf("closure missing table %q, got %v", want, meta.Subset.Tables)
		}
	}
	if tableSet["empty_audit"] {
		t.Fatal("empty_audit should not be in subset closure")
	}

	for _, name := range []string{"departments", "tbl_a"} {
		if _, err := os.Stat(tableDataPath(dir, metadataTable(t, meta, name))); err != nil {
			t.Fatalf("missing data for %s: %v", name, err)
		}
	}
	if tableSet["projects"] {
		if _, err := os.Stat(tableDataPath(dir, metadataTable(t, meta, "projects"))); err != nil {
			t.Fatalf("missing data for projects: %v", err)
		}
	}

	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "tbl_a")))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("tbl_a.ndjson is empty")
	}
}

func TestIntegrationSubsetDumpMaxTablesExceeded(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)},
		},
		Limits: SubsetLimits{MaxDepth: 10, MaxTables: 1, MaxRows: 100000, MaxInListSize: 500},
	}

	err := Dump(ctx, conn, dir, WithSubset(cfg))
	if err == nil {
		t.Fatal("expected max_tables error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not be promoted on subset limit failure")
	}
}

func TestIntegrationSubsetCappedChildDeterministicRepeated(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	ordersTable := fmt.Sprintf("det_orders_%d", suffix)
	itemsTable := fmt.Sprintf("det_items_%d", suffix)

	for _, q := range []string{
		fmt.Sprintf(`CREATE TABLE %s (id SERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`, ordersTable),
		fmt.Sprintf(`CREATE TABLE %s (id SERIAL PRIMARY KEY, order_id INTEGER NOT NULL REFERENCES %s(id), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`, itemsTable, ordersTable),
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s, %s CASCADE`, itemsTable, ordersTable))
	})

	var orderID int64
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO %s DEFAULT VALUES RETURNING id`, ordersTable)).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (order_id) VALUES ($1)`, itemsTable), orderID); err != nil {
			t.Fatal(err)
		}
	}

	const capPerTable = 2
	tables := ordersItemsSubsetTables(ordersTable, itemsTable)
	cfg := cappedPercentSubsetConfig(capPerTable)
	plan1, err := planSubset(ctx, conn, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan2, err := planSubset(ctx, conn, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalSubsetPlanFingerprint(plan1), canonicalSubsetPlanFingerprint(plan2)) {
		t.Fatal("plan fingerprints differ on PG16")
	}
	if len(plan1.tables[tableKey("public", itemsTable)].pkValues) != capPerTable {
		t.Fatalf("%s pk count = %d, want %d", itemsTable, len(plan1.tables[tableKey("public", itemsTable)].pkValues), capPerTable)
	}
}

func TestIntegrationDumpMetadataMatchesSchema(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir); err != nil {
		t.Fatal(err)
	}

	schema, err := db.LoadPostgresPublicSchema(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if len(meta.Tables) != len(schema) {
		t.Fatalf("metadata tables %d, schema %d", len(meta.Tables), len(schema))
	}
}

func TestIntegrationDumpChunkTableNoSafeKeyFallsBackToNormalStream(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	tableName := fmt.Sprintf("chunk_nopk_%d", time.Now().UnixNano())

	_, err := conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s (note TEXT NOT NULL)`, tableName,
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tableName))
	})

	const noteA = "fallback-row-a"
	const noteB = "fallback-row-b"
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (note) VALUES ($1), ($2)`, tableName,
	), noteA, noteB); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := Dump(ctx, conn, dir,
		WithoutSequences(),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: tableName}}),
		WithProvenance(Provenance{}),
	); err != nil {
		t.Fatalf("dump failed: %v", err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	qualified := "public." + tableName
	if meta.Provenance == nil || meta.Provenance.ChunkTables == nil {
		t.Fatal("expected chunk_tables provenance")
	}
	chunkProv := meta.Provenance.ChunkTables
	if len(chunkProv.Chunked) != 0 {
		t.Fatalf("chunked = %v, want none for no-safe-key table", chunkProv.Chunked)
	}
	if len(chunkProv.Fallback) != 1 || chunkProv.Fallback[0] != qualified {
		t.Fatalf("fallback = %v, want [%s]", chunkProv.Fallback, qualified)
	}

	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, tableName)))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("row count = %d, want 2", len(lines))
	}
	seen := make(map[string]struct{})
	for _, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		note, ok := row["note"].(string)
		if !ok {
			t.Fatalf("note type %T", row["note"])
		}
		seen[note] = struct{}{}
	}
	for _, want := range []string{noteA, noteB} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("missing row note %q, got %v", want, seen)
		}
	}

	chunkTempPath := tableDataPath(dir, metadataTable(t, meta, tableName)) + ".tmp"
	_, statErr := os.Stat(chunkTempPath)
	if statErr == nil {
		t.Fatalf("unexpected chunk temp file for normal-stream fallback: %s", chunkTempPath)
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat chunk temp %q: %v", chunkTempPath, statErr)
	}
}

func TestIntegrationDumpChunkTableResumeNoDuplicates(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	const chunkSize = 2
	const resumeAfter = 3

	policy := SelectionPolicy{
		Includes: []SelectorEntry{{Table: QualifiedTable{Schema: "public", Name: "tbl_a"}}},
	}
	opts := []Option{
		WithSlowChunkSize(chunkSize),
		WithTableSelection(policy, nil),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "tbl_a"}}),
		WithoutSequences(),
		WithProvenance(Provenance{}),
	}

	refDir := t.TempDir()
	if err := Dump(ctx, conn, refDir, opts...); err != nil {
		t.Fatal(err)
	}
	refMeta, err := ReadMetadata(refDir)
	if err != nil {
		t.Fatal(err)
	}
	refLines, err := readNDJSONLines(tableDataPath(refDir, metadataTable(t, refMeta, "tbl_a")))
	if err != nil {
		t.Fatal(err)
	}
	if len(refLines) <= resumeAfter {
		t.Fatalf("fixture tbl_a has %d rows, need > %d for resume test", len(refLines), resumeAfter)
	}

	var lastRow map[string]any
	if err := json.Unmarshal([]byte(refLines[resumeAfter-1]), &lastRow); err != nil {
		t.Fatal(err)
	}
	lastID, ok := lastRow["id"].(float64)
	if !ok {
		t.Fatalf("checkpoint id type %T", lastRow["id"])
	}

	dir := t.TempDir()
	table := metadataTable(t, refMeta, "tbl_a")
	tmpPath := tableDataPath(dir, table) + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0o700); err != nil {
		t.Fatal(err)
	}
	partial := strings.Join(refLines[:resumeAfter], "\n") + "\n"
	if err := os.WriteFile(tmpPath, []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSlowCheckpoint(slowCheckpointPath(dir, table), table, SelectKeyDescriptor(table), []any{int64(lastID)}); err != nil {
		t.Fatal(err)
	}

	if err := Dump(ctx, conn, dir, opts...); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "tbl_a")))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(refLines) {
		t.Fatalf("resumed row count = %d, want %d", len(lines), len(refLines))
	}
	seen := make(map[string]struct{})
	for _, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprint(row["id"])
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate row id %s after resumed chunk dump", id)
		}
		seen[id] = struct{}{}
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{"public.tbl_a"},
		[]KeyStrategy{KeyStrategyPrimaryKey},
		[]bool{true},
	)
}

func TestIntegrationDumpChunkTableUniqueKeyResume(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := integrationIsolatedSchema(t, conn)
	tableName := "uq_resume"
	qualified := schema + "." + tableName
	integrationCreateUniqueCodeTable(t, ctx, conn, schema, tableName, map[string]string{
		"a": "one", "b": "two", "c": "three", "d": "four", "e": "five",
	})

	const resumeAfter = 3
	opts := integrationIsolatedChunkOpts(schema, tableName, 2, &Provenance{Schemas: []string{schema}})
	refTable, refLines := integrationRefDumpLines(t, ctx, conn, opts, tableName)
	if len(refLines) <= resumeAfter {
		t.Fatalf("fixture has %d rows, need > %d", len(refLines), resumeAfter)
	}

	var lastRow map[string]any
	if err := json.Unmarshal([]byte(refLines[resumeAfter-1]), &lastRow); err != nil {
		t.Fatal(err)
	}
	lastCode, ok := lastRow["code"].(string)
	if !ok {
		t.Fatalf("checkpoint code type %T", lastRow["code"])
	}

	catalogTable := integrationCatalogTable(t, conn, schema, tableName)
	descriptor := SelectKeyDescriptor(catalogTable)
	if descriptor.Strategy != KeyStrategyUniqueIndex || descriptor.Fingerprint == "" {
		t.Fatalf("descriptor = %+v, want unique_index with fingerprint", descriptor)
	}

	dir := t.TempDir()
	tmpPath := tableDataPath(dir, refTable) + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpPath, []byte(strings.Join(refLines[:resumeAfter], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSlowCheckpoint(slowCheckpointPath(dir, catalogTable), catalogTable, descriptor, []any{lastCode}); err != nil {
		t.Fatal(err)
	}
	if err := Dump(ctx, conn, dir, opts...); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, tableName)))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(refLines) {
		t.Fatalf("resumed row count = %d, want %d", len(lines), len(refLines))
	}
	integrationAssertColumnUnique(t, lines, "code")
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{qualified},
		[]KeyStrategy{KeyStrategyUniqueIndex},
		[]bool{true},
	)
	rec := meta.Provenance.Strategies[0]
	if !slices.Equal(rec.KeyColumns, descriptor.ColumnNames()) || rec.Fingerprint != descriptor.Fingerprint {
		t.Fatalf("provenance identity = %+v, want columns %v fingerprint %q", rec, descriptor.ColumnNames(), descriptor.Fingerprint)
	}
	integrationAssertNoSlowArtifacts(t, dir, catalogTable)
}

func TestIntegrationDumpChunkTableChangedUniqueKeyRejects(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := integrationIsolatedSchema(t, conn)
	tableName := "uq_change"
	integrationCreateUniqueCodeTable(t, ctx, conn, schema, tableName, map[string]string{"a": "one", "b": "two", "c": "three"})

	opts := integrationIsolatedChunkOpts(schema, tableName, 2, nil)
	refTable, refLines := integrationRefDumpLines(t, ctx, conn, opts, tableName)
	if len(refLines) < 2 {
		t.Fatalf("fixture has %d rows, need >= 2", len(refLines))
	}

	var lastRow map[string]any
	if err := json.Unmarshal([]byte(refLines[0]), &lastRow); err != nil {
		t.Fatal(err)
	}
	lastCode, ok := lastRow["code"].(string)
	if !ok {
		t.Fatalf("checkpoint code type %T", lastRow["code"])
	}

	catalogTable := integrationCatalogTable(t, conn, schema, tableName)
	originalDesc := SelectKeyDescriptor(catalogTable)
	withFiles := integrationTableWithDataFile(catalogTable)

	dir := t.TempDir()
	finalPath := tableDataPath(dir, withFiles)
	tmpPath := tableDataPath(dir, refTable) + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0o700); err != nil {
		t.Fatal(err)
	}
	partial := []byte(refLines[0] + "\n")
	if err := os.WriteFile(tmpPath, partial, 0o600); err != nil {
		t.Fatal(err)
	}
	ckptPath := slowCheckpointPath(dir, catalogTable)
	if err := saveSlowCheckpoint(ckptPath, catalogTable, originalDesc, []any{lastCode}); err != nil {
		t.Fatal(err)
	}
	wantCkpt, err := os.ReadFile(ckptPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		fmt.Sprintf(`ALTER TABLE %s.%s DROP CONSTRAINT %s_code_key`, schema, tableName, tableName),
		fmt.Sprintf(`ALTER TABLE %s.%s ADD COLUMN aux integer NOT NULL DEFAULT 0`, schema, tableName),
		fmt.Sprintf(`CREATE UNIQUE INDEX %s_code_aux_key ON %s.%s (code, aux)`, tableName, schema, tableName),
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	err = Dump(ctx, conn, dir, opts...)
	if err == nil {
		t.Fatal("want checkpoint fingerprint mismatch error")
	}
	for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
		if unwrapped.Error() == "checkpoint fingerprint mismatch" {
			if !strings.Contains(err.Error(), "load checkpoint for table") || !strings.Contains(err.Error(), tableName) {
				t.Fatalf("error = %v, want wrapped fingerprint mismatch for table %q", err, tableName)
			}
			goto mismatchOK
		}
	}
	t.Fatalf("error = %v, want checkpoint fingerprint mismatch", err)
mismatchOK:
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not be promoted on descriptor mismatch")
	}
	if _, statErr := os.Stat(finalPath); statErr == nil {
		t.Fatal("final data file should not be promoted on descriptor mismatch")
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat final data %q: %v", finalPath, statErr)
	}
	gotCkpt, err := os.ReadFile(ckptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotCkpt, wantCkpt) {
		t.Fatal("checkpoint bytes changed after rejection")
	}
	gotTmp, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotTmp, partial) {
		t.Fatal("temp data bytes changed after rejection")
	}
}

func TestIntegrationDumpSlowMixedSchemaStrategies(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	schema := integrationIsolatedSchema(t, conn)

	for _, q := range []string{
		fmt.Sprintf(`CREATE TABLE %s.pk_items (id SERIAL PRIMARY KEY, val TEXT NOT NULL)`, schema),
		fmt.Sprintf(`CREATE TABLE %s.uq_codes (code TEXT NOT NULL UNIQUE, note TEXT NOT NULL)`, schema),
		fmt.Sprintf(`CREATE TABLE %s.fallback_logs (note TEXT)`, schema),
	} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.pk_items (val) VALUES ('pk1'), ('pk2')`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.uq_codes (code, note) VALUES ('aa','u1'), ('bb','u2')`, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.fallback_logs (note) VALUES ('log1'), ('log2')`, schema)); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	stderr := captureStderr(func() {
		if err := Dump(ctx, conn, dir,
			WithSchemas([]string{schema}),
			WithSlowConnection(),
			WithoutSequences(),
			WithProvenance(Provenance{Schemas: []string{schema}}),
		); err != nil {
			t.Fatal(err)
		}
	})
	fallbackQualified := schema + ".fallback_logs"
	wantWarn := "warning: table \"" + fallbackQualified + "\" has no safe key; using non-resumable normal streaming"
	if strings.TrimSpace(stderr) != wantWarn {
		t.Fatalf("stderr = %q, want exactly %q", stderr, wantWarn)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertStrategyRecords(t, meta.Provenance.Strategies,
		[]string{schema + ".fallback_logs", schema + ".pk_items", schema + ".uq_codes"},
		[]KeyStrategy{KeyStrategyNormalStream, KeyStrategyPrimaryKey, KeyStrategyUniqueIndex},
		[]bool{false, true, true},
	)

	pkLines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "pk_items")))
	if err != nil {
		t.Fatal(err)
	}
	if len(pkLines) != 2 {
		t.Fatalf("pk_items rows = %d, want 2", len(pkLines))
	}
	integrationAssertColumnUnique(t, pkLines, "id")

	uqLines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "uq_codes")))
	if err != nil {
		t.Fatal(err)
	}
	if len(uqLines) != 2 {
		t.Fatalf("uq_codes rows = %d, want 2", len(uqLines))
	}
	integrationAssertColumnUnique(t, uqLines, "code")

	fallbackLines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, "fallback_logs")))
	if err != nil {
		t.Fatal(err)
	}
	seenNotes := map[string]int{}
	for _, line := range fallbackLines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		note, _ := row["note"].(string)
		seenNotes[note]++
	}
	for _, want := range []string{"log1", "log2"} {
		if seenNotes[want] != 1 {
			t.Fatalf("fallback note %q count=%d, want 1", want, seenNotes[want])
		}
	}

	for _, name := range []string{"pk_items", "uq_codes", "fallback_logs"} {
		table := integrationCatalogTable(t, conn, schema, name)
		integrationAssertNoSlowArtifacts(t, dir, table)
	}
}

func integrationIsolatedSchema(t *testing.T, conn *sql.DB) string {
	t.Helper()
	schema := fmt.Sprintf("dolly_dump_%d", time.Now().UnixNano())
	if _, err := conn.ExecContext(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	return schema
}

func integrationIsolatedChunkOpts(schema, table string, chunk int, prov *Provenance) []Option {
	opts := []Option{
		WithSchemas([]string{schema}),
		WithSlowChunkSize(chunk),
		WithTableSelection(SelectionPolicy{Includes: []SelectorEntry{{Table: QualifiedTable{Schema: schema, Name: table}}}}, nil),
		WithChunkTables([]QualifiedTable{{Schema: schema, Name: table}}),
		WithoutSequences(),
	}
	if prov != nil {
		opts = append(opts, WithProvenance(*prov))
	}
	return opts
}

func integrationCreateUniqueCodeTable(t *testing.T, ctx context.Context, conn *sql.DB, schema, name string, rows map[string]string) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE %s.%s (code TEXT NOT NULL UNIQUE, note TEXT NOT NULL)`, schema, name,
	)); err != nil {
		t.Fatal(err)
	}
	for code, note := range rows {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(
			`INSERT INTO %s.%s (code, note) VALUES ($1, $2)`, schema, name,
		), code, note); err != nil {
			t.Fatal(err)
		}
	}
}

func integrationRefDumpLines(t *testing.T, ctx context.Context, conn *sql.DB, opts []Option, tableName string) (db.Table, []string) {
	t.Helper()
	refDir := t.TempDir()
	if err := Dump(ctx, conn, refDir, opts...); err != nil {
		t.Fatal(err)
	}
	refMeta, err := ReadMetadata(refDir)
	if err != nil {
		t.Fatal(err)
	}
	refTable := metadataTable(t, refMeta, tableName)
	refLines, err := readNDJSONLines(tableDataPath(refDir, refTable))
	if err != nil {
		t.Fatal(err)
	}
	return refTable, refLines
}

func integrationCatalogTable(t *testing.T, conn *sql.DB, schema, name string) db.Table {
	t.Helper()
	tables, err := db.LoadPostgresSchemas(context.Background(), conn, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table.Schema == schema && table.Name == name {
			return table
		}
	}
	t.Fatalf("table %s.%s not in catalog", schema, name)
	return db.Table{}
}

func integrationAssertColumnUnique(t *testing.T, lines []string, column string) {
	t.Helper()
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		val := fmt.Sprint(row[column])
		if _, dup := seen[val]; dup {
			t.Fatalf("duplicate %s=%q", column, val)
		}
		seen[val] = struct{}{}
	}
}

func integrationAssertNoSlowArtifacts(t *testing.T, dir string, tables ...db.Table) {
	t.Helper()
	for _, table := range tables {
		withFiles := integrationTableWithDataFile(table)
		for _, path := range []string{
			slowCheckpointPath(dir, withFiles),
			slowCheckpointPath(dir, withFiles) + ".tmp",
			tableDataPath(dir, withFiles) + ".tmp",
		} {
			if _, err := os.Stat(path); err == nil {
				t.Fatalf("unexpected artifact %s", path)
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stat %s: %v", path, err)
			}
		}
	}
}

func integrationTableWithDataFile(table db.Table) db.Table {
	tables := []db.Table{table}
	assignDataFiles(tables)
	return tables[0]
}

func TestIntegrationParallelDumpSharedSnapshot(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	conn.SetMaxOpenConns(10)

	suffix := time.Now().UnixNano()
	tableA := fmt.Sprintf("parallel_snap_a_%d", suffix)
	tableB := fmt.Sprintf("parallel_snap_b_%d", suffix)
	for _, name := range []string{tableA, tableB} {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(
			`CREATE TABLE %s (id BIGINT PRIMARY KEY, name TEXT NOT NULL)`, name,
		)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name))
		})
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(
			`INSERT INTO %s (id, name) VALUES (1, 'before'), (2, 'before')`, name,
		)); err != nil {
			t.Fatal(err)
		}
	}

	const markerA int64 = 900_000_001
	const markerB int64 = 900_000_002

	policy := SelectionPolicy{
		Includes: []SelectorEntry{
			{Table: QualifiedTable{Schema: "public", Name: tableA}},
			{Table: QualifiedTable{Schema: "public", Name: tableB}},
		},
	}

	snapReady := make(chan struct{})
	parallelTestHooks.onSnapshotExported = func() { close(snapReady) }
	t.Cleanup(func() { parallelTestHooks.onSnapshotExported = nil })

	dir := t.TempDir()
	errCh := make(chan error, 1)
	go func() {
		errCh <- Dump(ctx, conn, dir, WithoutSequences(), WithWorkers(2), WithTableSelection(policy, nil))
	}()

	<-snapReady

	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, name) VALUES ($1, 'after-snapshot')`, tableA,
	), markerA); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s (id, name) VALUES ($1, 'after-snapshot')`, tableB,
	), markerB); err != nil {
		t.Fatal(err)
	}

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tableName := range []string{tableA, tableB} {
		lines, err := readNDJSONLines(tableDataPath(dir, metadataTable(t, meta, tableName)))
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 2 {
			t.Fatalf("%s row count = %d, want 2 pre-snapshot rows", tableName, len(lines))
		}
		seen := make(map[int64]struct{})
		for _, line := range lines {
			var row map[string]any
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatal(err)
			}
			id, ok := row["id"].(float64)
			if !ok {
				t.Fatalf("%s id type %T", tableName, row["id"])
			}
			rowID := int64(id)
			if rowID == markerA || rowID == markerB {
				t.Fatalf("%s included post-snapshot row id %d", tableName, rowID)
			}
			seen[rowID] = struct{}{}
		}
		for _, want := range []int64{1, 2} {
			if _, ok := seen[want]; !ok {
				t.Fatalf("%s missing pre-snapshot row id %d", tableName, want)
			}
		}
	}
}

func TestIntegrationParallelDumpCancelCleanup(t *testing.T) {
	conn := openIntegrationDB(t)
	conn.SetMaxOpenConns(10)
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var startOnce sync.Once

	errCh := make(chan error, 1)
	go func() {
		errCh <- Dump(ctx, conn, dir,
			WithoutSequences(),
			WithWorkers(2),
			WithProgress(func(ev ProgressEvent) {
				if ev.Phase != "table_start" {
					return
				}
				startOnce.Do(func() {
					close(started)
					cancel()
				})
			}),
		)
	}()

	<-started
	err := <-errCh
	if err == nil {
		t.Fatal("expected cancelled parallel dump error")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist after cancelled dump")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ndjson") {
			t.Fatalf("final table file should not exist after cancel: %s", e.Name())
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, parallelStagingPrefix+"*")); len(matches) > 0 {
		t.Fatal("parallel staging should not remain after cancelled dump")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn.Stats().InUse == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pool in-use = %d after cancel cleanup", conn.Stats().InUse)
}

func TestIntegrationParallelDumpDefaultSerialRegression(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir, WithoutSequences()); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Tables) == 0 {
		t.Fatal("expected tables in metadata")
	}
}

func TestIntegrationDumpSelectedSchemasAlign(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()
	schemas := []string{"app", "public"}

	if err := Dump(ctx, conn, dir,
		WithSchemas(schemas),
		WithSlowConnection(),
		WithChunkTables([]QualifiedTable{{Schema: "public", Name: "tbl_a"}}),
		WithProvenance(Provenance{Schemas: append([]string(nil), schemas...)}),
	); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Schema != "multi" {
		t.Fatalf("schema label = %q, want multi", meta.Schema)
	}
	if meta.Provenance == nil {
		t.Fatal("missing provenance")
	}
	if !schemaSlicesEqual(meta.Provenance.Schemas, schemas) {
		t.Fatalf("provenance schemas = %v, want %v", meta.Provenance.Schemas, schemas)
	}

	names := map[string]bool{}
	for _, tbl := range meta.Tables {
		if tbl.Schema != "app" && tbl.Schema != "public" {
			t.Fatalf("table %s.%s outside selected schemas", tbl.Schema, tbl.Name)
		}
		names[tbl.Schema+"."+tbl.Name] = true
	}
	for _, want := range []string{"public.tbl_a", "app.invoices"} {
		if !names[want] {
			t.Fatalf("table set = %v, missing %s", names, want)
		}
	}
	if len(meta.Sequences) == 0 {
		t.Fatal("expected captured sequences for selected schemas")
	}
	assertSequencesInSchemas(t, meta.Sequences, "app", "public")
	hasAppSeq, hasPublicSeq := false, false
	for _, seq := range meta.Sequences {
		switch seq.Schema {
		case "app":
			hasAppSeq = true
		case "public":
			hasPublicSeq = true
		}
	}
	if !hasAppSeq || !hasPublicSeq {
		t.Fatalf("sequences = %+v, want both app and public", meta.Sequences)
	}
}

func TestIntegrationDumpSlowConnectionExplicitPublicSchemas(t *testing.T) {
	conn := openIntegrationDB(t)
	dir := t.TempDir()
	ctx := context.Background()

	if err := Dump(ctx, conn, dir,
		WithSchemas([]string{"public"}),
		WithSlowConnection(),
		WithProvenance(Provenance{Schemas: []string{"public"}}),
	); err != nil {
		t.Fatal(err)
	}

	meta, err := ReadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range meta.Tables {
		if tbl.Schema != "public" {
			t.Fatalf("table %s in schema %q, want public only", tbl.Name, tbl.Schema)
		}
	}
	if meta.Provenance == nil || !schemaSlicesEqual(meta.Provenance.Schemas, []string{"public"}) {
		t.Fatalf("provenance schemas = %v, want [public]", meta.Provenance)
	}
	assertSequencesInSchemas(t, meta.Sequences, "public")
	if _, err := os.Stat(tableDataPath(dir, metadataTable(t, meta, "tbl_a"))); err != nil {
		t.Fatalf("missing tbl_a data: %v", err)
	}
}

func schemaSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertSequencesInSchemas(t *testing.T, seqs []SequenceState, allowed ...string) {
	t.Helper()
	set := make(map[string]struct{}, len(allowed))
	for _, s := range allowed {
		set[s] = struct{}{}
	}
	for _, seq := range seqs {
		if _, ok := set[seq.Schema]; !ok {
			t.Fatalf("sequence %s.%s outside allowed schemas %v", seq.Schema, seq.Name, allowed)
		}
	}
}

func TestIntegrationDumpRejectsEmptySelectedSchema(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	const schemaName = "dolly_empty_guard"
	if _, err := conn.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
	})

	dir := t.TempDir()
	err := Dump(ctx, conn, dir, WithSchemas([]string{schemaName}), WithoutSequences())
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist for empty schema dump")
	}
}

func TestIntegrationDumpExcludeAllFailsBeforeOutput(t *testing.T) {
	conn := openIntegrationDB(t)
	ctx := context.Background()
	dir := t.TempDir()

	excludes := []SelectorEntry{
		{Table: QualifiedTable{Schema: "public", Name: "departments"}},
		{Table: QualifiedTable{Schema: "public", Name: "tbl_a"}},
		{Table: QualifiedTable{Schema: "public", Name: "projects"}},
		{Table: QualifiedTable{Schema: "public", Name: "project_members"}},
		{Table: QualifiedTable{Schema: "public", Name: "empty_audit"}},
		{Table: QualifiedTable{Schema: "app", Name: "invoices"}},
	}
	err := Dump(ctx, conn, dir, WithoutSequences(), WithTableSelection(SelectionPolicy{Excludes: excludes}, nil))
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v, want NoTablesError", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist when all tables excluded")
	}
}
