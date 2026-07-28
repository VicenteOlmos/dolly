//go:build integration

package dump

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

func TestIntegrationDumpChunkTableNoPKFailsBeforeOutput(t *testing.T) {
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

	dir := t.TempDir()
	err = Dump(ctx, conn, dir, WithChunkTables([]QualifiedTable{{Schema: "public", Name: tableName}}))
	if err == nil || !IsChunkPolicyError(err) {
		t.Fatalf("error = %v, want chunk policy failure", err)
	}
	if !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "metadata.json")); statErr == nil {
		t.Fatal("metadata.json should not exist when chunk preflight fails")
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
	partial := strings.Join(refLines[:resumeAfter], "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "tbl_a.ndjson.tmp"), []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}
	table := metadataTable(t, refMeta, "tbl_a")
	if err := saveSlowCheckpoint(checkpointPath(dir, "tbl_a"), table, []string{"id"}, []any{int64(lastID)}); err != nil {
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
}
