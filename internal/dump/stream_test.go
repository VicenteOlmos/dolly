package dump

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func streamTableSlowDefault(ctx context.Context, q querier, table db.Table, dir string, rowTransform RowTransform) error {
	return streamTableSlow(ctx, q, table, dir, rowTransform, slowRetryConfig{}, DefaultSlowChunkSize)
}

// slowQuerySQL builds the exact SELECT streamTableSlow issues for keyset pagination.
func slowQuerySQL(table db.Table, chunkSize int, resumeKeyArity int) string {
	descriptor := SelectKeyDescriptor(table)
	keyCols := descriptor.ColumnNames()

	cols := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		cols[i] = pgx.Identifier{c.Name}.Sanitize()
	}
	keyIdents := make([]string, len(keyCols))
	for i, k := range keyCols {
		keyIdents[i] = pgx.Identifier{k}.Sanitize()
	}
	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	colList := strings.Join(cols, ", ")
	keyOrder := strings.Join(keyIdents, ", ")

	if resumeKeyArity == 0 {
		return fmt.Sprintf("SELECT %s FROM %s ORDER BY %s LIMIT %d", colList, tableIdent, keyOrder, chunkSize)
	}
	placeholders := make([]string, resumeKeyArity)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE (%s) > (%s) ORDER BY %s LIMIT %d",
		colList, tableIdent, keyOrder, strings.Join(placeholders, ", "), keyOrder, chunkSize)
}

func slowQueryPattern(table db.Table, chunkSize int, resumeKeyArity int) string {
	return "^" + regexp.QuoteMeta(slowQuerySQL(table, chunkSize, resumeKeyArity)) + "$"
}

func TestStreamTable(t *testing.T) {
	tests := []struct {
		name    string
		table   db.Table
		rows    *sqlmock.Rows
		want    []string
		wantErr bool
	}{
		{
			name: "int text null",
			table: db.Table{
				Schema: "public",
				Name:   "users",
				Columns: []db.Column{
					{Name: "id", DataType: "integer"},
					{Name: "name", DataType: "text"},
					{Name: "email", DataType: "text"},
				},
			},
			rows: sqlmock.NewRows([]string{"id", "name", "email"}).
				AddRow(1, "v1", nil).
				AddRow(2, "v2", "e2@x.test"),
			want: []string{
				`{"email":null,"id":1,"name":"v1"}`,
				`{"email":"e2@x.test","id":2,"name":"v2"}`,
			},
		},
		{
			name: "empty table",
			table: db.Table{
				Schema: "public",
				Name:   "empty",
				Columns: []db.Column{
					{Name: "id", DataType: "integer"},
				},
			},
			rows: sqlmock.NewRows([]string{"id"}),
			want: nil,
		},
		{
			name: "bool type",
			table: db.Table{
				Schema: "public",
				Name:   "flags",
				Columns: []db.Column{
					{Name: "active", DataType: "boolean"},
				},
			},
			rows: sqlmock.NewRows([]string{"active"}).
				AddRow(true).
				AddRow(false),
			want: []string{
				`{"active":true}`,
				`{"active":false}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()

			mock.ExpectQuery("SELECT .* FROM .*").
				WillReturnRows(tt.rows)

			dir := t.TempDir()
			err = streamTable(context.Background(), sqlDB, tt.table, dir, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("streamTable() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(dir, tt.table.Name+".ndjson"))
			if err != nil {
				t.Fatal(err)
			}

			if len(tt.want) == 0 {
				if len(data) != 0 {
					t.Fatalf("expected empty file, got %q", string(data))
				}
				return
			}

			gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(gotLines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d", len(gotLines), len(tt.want))
			}

			for i, want := range tt.want {
				if gotLines[i] != want {
					t.Fatalf("line %d: got %s, want %s", i, gotLines[i], want)
				}
			}
		})
	}
}

func TestStreamTableQueryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnError(context.Canceled)

	dir := t.TempDir()
	table := db.Table{
		Schema:  "public",
		Name:    "users",
		Columns: []db.Column{{Name: "id", DataType: "integer"}},
	}

	err = streamTable(context.Background(), sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "query table") {
		t.Fatalf("error missing context: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "users.ndjson.tmp")); !os.IsNotExist(err) {
		t.Fatal("expected no tmp file on query error")
	}
}

func TestStreamTableRowsError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).
		RowError(0, context.Canceled)

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(rows)

	dir := t.TempDir()
	table := db.Table{
		Schema:  "public",
		Name:    "users",
		Columns: []db.Column{{Name: "id", DataType: "integer"}},
	}

	err = streamTable(context.Background(), sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "iterate rows") {
		t.Fatalf("error missing context: %v", err)
	}
}

func TestStreamTableContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	dir := t.TempDir()
	table := db.Table{
		Schema:  "public",
		Name:    "users",
		Columns: []db.Column{{Name: "id", DataType: "integer"}},
	}

	err = streamTable(ctx, sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	if _, err := os.Stat(filepath.Join(dir, "users.ndjson.tmp")); !os.IsNotExist(err) {
		t.Fatal("expected no tmp file on cancelled context")
	}
}

func TestStreamTableManyRows(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "big",
		Columns: []db.Column{
			{Name: "n", DataType: "integer"},
		},
	}

	rows := sqlmock.NewRows([]string{"n"})
	want := make([]string, 500)
	for i := 0; i < 500; i++ {
		rows.AddRow(i)
		want[i] = fmt.Sprintf(`{"n":%d}`, i)
	}

	mock.ExpectQuery("SELECT .* FROM .*").
		WillReturnRows(rows)

	dir := t.TempDir()
	err = streamTable(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "big.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(gotLines), len(want))
	}
	for i, wantLine := range want {
		if gotLines[i] != wantLine {
			t.Fatalf("line %d: got %s, want %s", i, gotLines[i], wantLine)
		}
	}
}

func TestValidateTableName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "users", wantErr: false},
		{name: "valid with underscore", input: "my_table", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "dot only", input: ".", wantErr: true},
		{name: "double dot", input: "..", wantErr: true},
		{name: "forward slash", input: "etc/../passwd", wantErr: true},
		{name: "backslash", input: "etc\\passwd", wantErr: true},
		{name: "traversal prefix", input: "../etc", wantErr: true},
		{name: "traversal suffix", input: "etc/..", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTableName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTableName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestStreamTableSlowSinglePK(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	// First (and only) chunk: no WHERE, ORDER BY pk LIMIT 1000
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"id":1,"name":"v1"}
{"id":2,"name":"v2"}`
	got := strings.TrimSpace(string(data))
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStreamTableSlowNoPKError(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "no_pk_table",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
		},
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for table without primary key")
	}
	if !strings.Contains(err.Error(), "no resumable key") {
		t.Fatalf("error missing no-resumable-key context: %v", err)
	}
}

func streamUniqueTable() db.Table {
	return db.Table{
		Schema: "public", Name: "events",
		Columns: []db.Column{{Name: "code", DataType: "text", OrdinalPosition: 1}, {Name: "note", DataType: "text", OrdinalPosition: 2}},
		UniqueIndexes: []db.UniqueIndexInfo{{
			IndexSchema: "public", IndexName: "events_code_key", IndexOID: 20,
			IsValid: true, IsReady: true, AccessMethod: "btree",
			KeyColumns: []db.UniqueIndexColumn{{Name: "code", Position: 1, Attnum: 1, OpclassOID: 1978}},
		}},
	}
}

func TestStreamTableSlowUniqueIndexFirstPage(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := streamUniqueTable()
	mock.ExpectQuery(slowQueryPattern(table, DefaultSlowChunkSize, 0)).
		WithoutArgs().
		WillReturnRows(sqlmock.NewRows([]string{"code", "note"}).AddRow("a", "one").AddRow("b", "two"))

	dir := t.TempDir()
	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	want := `{"code":"a","note":"one"}
{"code":"b","note":"two"}`
	if got := strings.TrimSpace(readTableNDJSON(t, dir, "events")); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestStreamTableSlowUniqueIndexResume(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := streamUniqueTable()
	desc := SelectKeyDescriptor(table)
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "events.ndjson.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"code":"a","note":"one"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckpt, _ := json.Marshal(slowCheckpoint{
		Table: "events", Strategy: KeyStrategyUniqueIndex, KeyColumns: []string{"code"},
		KeyFingerprint: desc.Fingerprint, LastKey: []any{"a"},
	})
	if err := os.WriteFile(checkpointPath(dir, "events"), ckpt, 0o644); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(slowQueryPattern(table, DefaultSlowChunkSize, 1)).
		WithArgs("a").
		WillReturnRows(sqlmock.NewRows([]string{"code", "note"}).AddRow("b", "two"))

	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(readTableNDJSON(t, dir, "events")), "\n")
	if len(got) != 2 || got[1] != `{"code":"b","note":"two"}` {
		t.Fatalf("output = %v", got)
	}
}

func TestStreamTableSlowCompositeUniqueIndex(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public", Name: "pairs",
		Columns: []db.Column{
			{Name: "tenant", DataType: "text", OrdinalPosition: 1},
			{Name: "seq", DataType: "integer", OrdinalPosition: 2},
			{Name: "val", DataType: "text", OrdinalPosition: 3},
		},
		UniqueIndexes: []db.UniqueIndexInfo{{
			IndexSchema: "public", IndexName: "pairs_tenant_seq_key", IndexOID: 30,
			IsValid: true, IsReady: true, AccessMethod: "btree",
			KeyColumns: []db.UniqueIndexColumn{
				{Name: "tenant", Position: 1, Attnum: 1, OpclassOID: 1978},
				{Name: "seq", Position: 2, Attnum: 2, OpclassOID: 1978},
			},
		}},
	}
	desc := SelectKeyDescriptor(table)

	rows1 := sqlmock.NewRows([]string{"tenant", "seq", "val"}).
		AddRow("t1", 1, "a").AddRow("t1", 2, "b")
	mock.ExpectQuery(slowQueryPattern(table, 2, 0)).
		WithoutArgs().
		WillReturnRows(rows1)
	mock.ExpectQuery(slowQueryPattern(table, 2, 2)).
		WithArgs("t1", int64(2)).
		WillReturnError(fmt.Errorf("connection reset"))

	dir := t.TempDir()
	err = streamTableSlow(context.Background(), sqlDB, table, dir, nil, slowRetryConfig{}, 2)
	if err == nil {
		t.Fatal("expected query error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	ckptPath := slowCheckpointPath(dir, table)
	if _, err := os.Stat(ckptPath); err != nil {
		t.Fatal("checkpoint must persist after partial chunk failure")
	}
	ckpt, err := loadSlowCheckpoint(ckptPath)
	if err != nil {
		t.Fatal(err)
	}
	if ckpt.Strategy != KeyStrategyUniqueIndex || ckpt.KeyFingerprint != desc.Fingerprint ||
		!slices.Equal(ckpt.KeyColumns, []string{"tenant", "seq"}) || len(ckpt.LastKey) != 2 {
		t.Fatalf("checkpoint = %+v", ckpt)
	}
	if ckpt.LastKey[0] != "t1" {
		t.Fatalf("last key tenant = %v", ckpt.LastKey[0])
	}
	if n, ok := ckpt.LastKey[1].(json.Number); !ok || n.String() != "2" {
		t.Fatalf("last key seq = %v", ckpt.LastKey[1])
	}
}

func TestStreamTableSlowNormalStreamRejected(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public", Name: "unsafe",
		Columns: []db.Column{{Name: "code", DataType: "text", IsNullable: true, OrdinalPosition: 1}},
		UniqueIndexes: []db.UniqueIndexInfo{{
			IndexSchema: "public", IndexName: "unsafe_code_key", IndexOID: 10,
			IsValid: true, IsReady: true, AccessMethod: "btree",
			KeyColumns: []db.UniqueIndexColumn{{Name: "code", Position: 1, Attnum: 1, OpclassOID: 1978, IsNullable: true}},
		}},
	}
	err = streamTableSlowDefault(context.Background(), sqlDB, table, t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "no resumable key") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamTableSlowUniqueIndexDescriptorMismatch(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := streamUniqueTable()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson.tmp"), []byte(`{"code":"a","note":"one"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckpt, _ := json.Marshal(slowCheckpoint{
		Table: "events", Strategy: KeyStrategyUniqueIndex, KeyColumns: []string{"code"},
		KeyFingerprint: "deadbeef", LastKey: []any{"a"},
	})
	if err := os.WriteFile(checkpointPath(dir, "events"), ckpt, 0o644); err != nil {
		t.Fatal(err)
	}
	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected DB query before checkpoint validation: %v", err)
	}
}

func TestStreamTableSlowUniqueIndexCheckpointStrategyMismatchRejectsBeforeQuery(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := streamUniqueTable()
	desc := SelectKeyDescriptor(table)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson.tmp"), []byte(`{"code":"a","note":"one"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckpt, _ := json.Marshal(slowCheckpoint{
		Table:          "events",
		Strategy:       KeyStrategyPrimaryKey,
		KeyColumns:     []string{"code"},
		KeyFingerprint: desc.Fingerprint,
		LastKey:        []any{"a"},
	})
	if err := os.WriteFile(checkpointPath(dir, "events"), ckpt, 0o644); err != nil {
		t.Fatal(err)
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err == nil || !strings.Contains(err.Error(), "strategy mismatch") {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected DB query before checkpoint validation: %v", err)
	}
}

func readTableNDJSON(t *testing.T, dir, table string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, table+".ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStreamTableSlowCompositePKIntInt(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "pairs",
		Columns: []db.Column{
			{Name: "a", DataType: "integer", PrimaryKey: true},
			{Name: "b", DataType: "integer", PrimaryKey: true},
			{Name: "val", DataType: "text"},
		},
	}

	rows := sqlmock.NewRows([]string{"a", "b", "val"}).
		AddRow(1, 1, "x").
		AddRow(1, 2, "y")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "pairs.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":1,"b":1,"val":"x"}
{"a":1,"b":2,"val":"y"}`
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", string(data), want)
	}
}

func TestStreamTableSlowCompositePKIntTextResume(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "items",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "code", DataType: "text", PrimaryKey: true},
			{Name: "qty", DataType: "integer"},
		},
	}

	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "items.ndjson.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"code":"a","id":1,"qty":10}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptPath := checkpointPath(dir, "items")
	ckpt := slowCheckpoint{
		Table:     "items",
		PKColumns: []string{"id", "code"},
		LastPK:    []any{json.Number("1"), "a"},
	}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	resumeRows := sqlmock.NewRows([]string{"id", "code", "qty"}).
		AddRow(1, "b", 20)
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(1), "a").
		WillReturnRows(resumeRows)

	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "items.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != 2 {
		t.Fatalf("got %d lines, want 2", len(gotLines))
	}
	if gotLines[1] != `{"code":"b","id":1,"qty":20}` {
		t.Fatalf("last line: got %s", gotLines[1])
	}
}

func TestStreamTableSlowLegacyCheckpointDiscarded(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"id":1,"name":"v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptPath := checkpointPath(dir, "users")
	if err := os.WriteFile(ckptPath, []byte(`{"table":"users","pk_column":"id","last_pk":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"name":"v1"}
{"id":2,"name":"v2"}`
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", string(data), want)
	}
}

func TestStreamTableSlowConfigurableChunkSize(t *testing.T) {
	const chunkSize = 3

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	rows1 := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").AddRow(2, "v2").AddRow(3, "v3")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 3").
		WillReturnRows(rows1)

	rows2 := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(4, "v4").AddRow(5, "v5")
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 3").
		WithArgs(int64(3)).
		WillReturnRows(rows2)

	dir := t.TempDir()
	err = streamTableSlow(context.Background(), sqlDB, table, dir, nil, slowRetryConfig{}, chunkSize)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"name":"v1"}
{"id":2,"name":"v2"}
{"id":3,"name":"v3"}
{"id":4,"name":"v4"}
{"id":5,"name":"v5"}`
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", string(data), want)
	}
}

func TestStreamTableSlowMultiChunk(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	// Chunk 1: exactly DefaultSlowChunkSize rows → loop continues
	rows1 := sqlmock.NewRows([]string{"id", "name"})
	wantLines := make([]string, 0, DefaultSlowChunkSize+3)
	for i := 1; i <= DefaultSlowChunkSize; i++ {
		rows1.AddRow(i, fmt.Sprintf("v%d", i))
		wantLines = append(wantLines, fmt.Sprintf(`{"id":%d,"name":"v%d"}`, i, i))
	}
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows1)

	// Chunk 2: 3 rows (pk > DefaultSlowChunkSize)
	rows2 := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1001, "v1001").
		AddRow(1002, "v1002").
		AddRow(1003, "v1003")
	wantLines = append(wantLines,
		`{"id":1001,"name":"v1001"}`,
		`{"id":1002,"name":"v1002"}`,
		`{"id":1003,"name":"v1003"}`,
	)
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(DefaultSlowChunkSize)).
		WillReturnRows(rows2)

	dir := t.TempDir()
	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join(wantLines, "\n")
	got := strings.TrimSpace(string(data))
	if got != want {
		// Only compare counts for brevity on failure
		gotLines := strings.Split(got, "\n")
		if len(gotLines) != len(wantLines) {
			t.Fatalf("got %d lines, want %d", len(gotLines), len(wantLines))
		}
		t.Fatalf("first line:\n  got: %s\n want: %s", gotLines[0], wantLines[0])
	}
}

func TestStreamTableSlowQueryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnError(context.Canceled)

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "query table") || !strings.Contains(err.Error(), "chunk") {
		t.Fatalf("error missing chunk context: %v", err)
	}
}

func TestStreamTableSlowResumeFromCheckpoint(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()

	// Simulate previous interrupted run: partial .ndjson.tmp with rows 1-500.
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	var partialLines []string
	for i := 1; i <= 500; i++ {
		partialLines = append(partialLines, fmt.Sprintf(`{"id":%d,"name":"v%d"}`, i, i))
	}
	if err := os.WriteFile(tmpPath, []byte(strings.Join(partialLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write checkpoint at last PK = 500.
	ckptPath := checkpointPath(dir, "users")
	ckpt := slowCheckpoint{Table: "users", PKColumns: []string{"id"}, LastPK: []any{json.Number("500")}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Resume query: WHERE id > 500.
	resumeRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(501, "v501").
		AddRow(502, "v502")
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(500)).
		WillReturnRows(resumeRows)

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	// Verify final output: 502 lines, original rows preserved, no duplication.
	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != 502 {
		t.Fatalf("got %d lines, want 502", len(gotLines))
	}
	if gotLines[0] != `{"id":1,"name":"v1"}` {
		t.Fatalf("first line: got %s", gotLines[0])
	}
	if gotLines[501] != `{"id":502,"name":"v502"}` {
		t.Fatalf("last line: got %s", gotLines[501])
	}

	// Checkpoint must be removed after successful completion.
	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("checkpoint file should be removed after successful completion")
	}
}

func TestStreamTableSlowResumeCheckpointTempMismatch(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()

	// Temp file ends at pk=400, but checkpoint claims pk=500.
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	var partialLines []string
	for i := 1; i <= 400; i++ {
		partialLines = append(partialLines, fmt.Sprintf(`{"id":%d,"name":"v%d"}`, i, i))
	}
	if err := os.WriteFile(tmpPath, []byte(strings.Join(partialLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ckptPath := checkpointPath(dir, "users")
	ckpt := slowCheckpoint{Table: "users", PKColumns: []string{"id"}, LastPK: []any{json.Number("500")}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Mismatch resets artifacts and dumps from scratch.
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("stale ndjson temp should be removed after reset")
	}
	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint should be removed after successful dump")
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"name":"v1"}
{"id":2,"name":"v2"}`
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", string(data), want)
	}
}

func TestStreamTableSlowResumeEmptyTemp(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	if err := os.WriteFile(tmpPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ckptPath := checkpointPath(dir, "users")
	ckpt := slowCheckpoint{Table: "users", PKColumns: []string{"id"}, LastPK: []any{json.Number("500")}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Empty temp with checkpoint resets and dumps from scratch.
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("stale ndjson temp should be removed after reset")
	}
	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint should be removed after successful dump")
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1}
{"id":2}`
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", string(data), want)
	}
}

func TestStreamTableSlowCheckpointRemovedOnCompletion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	ckptPath := checkpointPath(dir, "users")
	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("checkpoint should be removed after successful completion")
	}

	finalPath := filepath.Join(dir, "users.ndjson")
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatal("final .ndjson should exist")
	}
}

func TestStreamTableSlowSkipCompletedTable(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	dir := t.TempDir()
	// Pre-create the final .ndjson so the table appears already completed.
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson"), []byte(`{"id":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should return nil without querying DB. sqlmock with no expectations
	// would fail if any SQL call were made.
	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamTableSlowPartialChunkFailureResumeNoDuplicates(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()

	// Run 1: chunk 1 fills the chunk size so chunk 2 is attempted, then chunk 2 fails mid-scan.
	rows1 := sqlmock.NewRows([]string{"id", "name"})
	for i := 1; i <= DefaultSlowChunkSize; i++ {
		rows1.AddRow(i, fmt.Sprintf("v%d", i))
	}
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows1)

	failingRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(DefaultSlowChunkSize+1, fmt.Sprintf("v%d", DefaultSlowChunkSize+1)).
		AddRow(DefaultSlowChunkSize+2, fmt.Sprintf("v%d", DefaultSlowChunkSize+2)).
		RowError(0, fmt.Errorf("simulate mid-chunk failure"))
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(DefaultSlowChunkSize)).
		WillReturnRows(failingRows)

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected error on partial chunk failure")
	}

	// Run 2: resume from checkpoint at pk=DefaultSlowChunkSize; chunk 2 succeeds.
	resumeRows := sqlmock.NewRows([]string{"id", "name"})
	for i := DefaultSlowChunkSize + 1; i <= DefaultSlowChunkSize+2; i++ {
		resumeRows.AddRow(i, fmt.Sprintf("v%d", i))
	}
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(DefaultSlowChunkSize)).
		WillReturnRows(resumeRows)

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != DefaultSlowChunkSize+2 {
		t.Fatalf("got %d lines, want %d", len(gotLines), DefaultSlowChunkSize+2)
	}
	if gotLines[0] != `{"id":1,"name":"v1"}` {
		t.Fatalf("first line: got %s", gotLines[0])
	}
	last := DefaultSlowChunkSize + 2
	wantLast := fmt.Sprintf(`{"id":%d,"name":"v%d"}`, last, last)
	if gotLines[len(gotLines)-1] != wantLast {
		t.Fatalf("last line: got %s, want %s", gotLines[len(gotLines)-1], wantLast)
	}
}

func TestStreamTableSlowBigintCheckpointPrecision(t *testing.T) {
	bigID := int64(9007199254740993)

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "bigids",
		Columns: []db.Column{
			{Name: "id", DataType: "bigint", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(bigID, "first"))

	dir := t.TempDir()
	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "bigids.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != `{"id":9007199254740993,"name":"first"}` {
		t.Fatalf("unexpected output: %s", string(data))
	}

	// Resume from a checkpoint written with the large bigint.
	resumeDir := t.TempDir()
	tmpPath := filepath.Join(resumeDir, "bigids.ndjson.tmp")
	if err := os.WriteFile(tmpPath, []byte(fmt.Sprintf(`{"id":%d,"name":"one"}`+"\n", bigID)), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptPath := checkpointPath(resumeDir, "bigids")
	ckpt := slowCheckpoint{Table: "bigids", PKColumns: []string{"id"}, LastPK: []any{json.Number(strconv.FormatInt(bigID, 10))}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}
	savePath := filepath.Join(t.TempDir(), "bigids.ckpt.json")
	saveDesc := SelectKeyDescriptor(table)
	if err := saveSlowCheckpoint(savePath, table, saveDesc, []any{bigID}); err != nil {
		t.Fatal(err)
	}
	saveData, _ := os.ReadFile(savePath)
	wantKey := fmt.Sprintf(`"last_key":[%d]`, bigID)
	if !strings.Contains(string(saveData), wantKey) {
		t.Fatalf("generalized save missing last_key: %s", saveData)
	}
	if strings.Contains(string(saveData), `"last_pk"`) {
		t.Fatalf("generalized save must not use last_pk: %s", saveData)
	}

	sqlDB2, mock2, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB2.Close()
	mock2.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(bigID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(bigID+1, "next"))

	if err := streamTableSlowDefault(context.Background(), sqlDB2, table, resumeDir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock2.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	final, err := os.ReadFile(filepath.Join(resumeDir, "bigids.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(final)), "\n")
	if len(gotLines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %s", len(gotLines), string(final))
	}
}

func TestStreamTableSlowCheckpointWithoutTempStartsFresh(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	ckptPath := checkpointPath(dir, "users")
	if err := os.WriteFile(ckptPath, []byte(`{"table":"users","pk_column":"id","last_pk":99}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint without temp should be removed")
	}

	data, err := os.ReadFile(filepath.Join(dir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != 2 || gotLines[0] != `{"id":1,"name":"v1"}` {
		t.Fatalf("unexpected output: %s", string(data))
	}
}

func TestStreamTableSlowCheckpointSaveFailureRollsBackChunk(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	// One chunk with two rows; checkpoint save will fail, so chunk must roll back.
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(1).AddRow(2)
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	// Pre-create the data temp file so the cleanup path does not remove our trap.
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson.tmp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make checkpoint temp path a directory so saveSlowCheckpoint's os.Create fails.
	ckptTmpPath := checkpointPath(dir, "users") + ".tmp"
	if err := os.Mkdir(ckptTmpPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected checkpoint save error")
	}
	if !strings.Contains(err.Error(), "save checkpoint") {
		t.Fatalf("error missing checkpoint context: %v", err)
	}

	// Temp file should be empty after rollback to the offset before the chunk.
	info, err := os.Stat(filepath.Join(dir, "users.ndjson.tmp"))
	if err != nil {
		t.Fatalf("expected temp file to exist: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("temp file size = %d, want 0 after rollback", info.Size())
	}
}

func TestStreamTableSlowResumeCheckpointSaveFailurePreservesPriorRows(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()

	// Simulate a previous run that committed rows 1-500.
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	var priorLines []string
	for i := 1; i <= 500; i++ {
		priorLines = append(priorLines, fmt.Sprintf(`{"id":%d,"name":"v%d"}`, i, i))
	}
	priorData := []byte(strings.Join(priorLines, "\n") + "\n")
	if err := os.WriteFile(tmpPath, priorData, 0o644); err != nil {
		t.Fatal(err)
	}

	ckptPath := checkpointPath(dir, "users")
	ckpt := slowCheckpoint{Table: "users", PKColumns: []string{"id"}, LastPK: []any{json.Number("500")}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Resume query returns the next chunk.
	resumeRows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(501, "v501").
		AddRow(502, "v502")
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(500)).
		WillReturnRows(resumeRows)

	// Make checkpoint temp path a directory so saveSlowCheckpoint fails.
	ckptTmpPath := ckptPath + ".tmp"
	if err := os.Mkdir(ckptTmpPath, 0o755); err != nil {
		t.Fatal(err)
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err == nil {
		t.Fatal("expected checkpoint save error")
	}
	if !strings.Contains(err.Error(), "save checkpoint") {
		t.Fatalf("error missing checkpoint context: %v", err)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		t.Fatalf("expected temp file to exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("temp file was truncated to 0; prior committed rows lost")
	}
	if info.Size() != int64(len(priorData)) {
		t.Fatalf("temp file size = %d, want %d", info.Size(), len(priorData))
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(gotLines) != 500 {
		t.Fatalf("got %d lines, want 500", len(gotLines))
	}
	if gotLines[0] != `{"id":1,"name":"v1"}` {
		t.Fatalf("first line: got %s", gotLines[0])
	}
}

func TestStreamTableSlowCompletedCleansTempArtifacts(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	dir := t.TempDir()
	finalPath := filepath.Join(dir, "users.ndjson")
	ckptPath := checkpointPath(dir, "users")
	if err := os.WriteFile(finalPath, []byte(`{"id":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ckptPath, []byte(`{"table":"users","pk_column":"id","last_pk":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ckptPath+".tmp", []byte(`incomplete`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "users.ndjson.tmp"), []byte(`{"id":2}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint should be removed")
	}
	if _, err := os.Stat(ckptPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint temp should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "users.ndjson.tmp")); !os.IsNotExist(err) {
		t.Fatal("stale ndjson temp should be removed")
	}
}

func TestStreamTableSlowCompletedSkipsNoPKAndCleansCheckpoint(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// Table intentionally has no primary key.
	table := db.Table{
		Schema: "public",
		Name:   "no_pk",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
		},
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "no_pk.ndjson"), []byte(`{"id":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptPath := checkpointPath(dir, "no_pk")
	if err := os.WriteFile(ckptPath, []byte(`{"table":"no_pk","pk_column":"","last_pk":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err = streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

	if _, err := os.Stat(ckptPath); !os.IsNotExist(err) {
		t.Fatal("stale checkpoint should be removed")
	}
}

type flakyQuerier struct {
	inner          querier
	failsRemaining int
	failErr        error
}

func (f *flakyQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if f.failsRemaining > 0 {
		f.failsRemaining--
		return nil, f.failErr
	}
	return f.inner.QueryContext(ctx, query, args...)
}

func TestStreamTableSlowRetrySucceedsAfterFailures(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "v1")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").
		WillReturnRows(rows)

	dir := t.TempDir()
	fq := &flakyQuerier{inner: sqlDB, failsRemaining: 2, failErr: fmt.Errorf("connection reset")}
	retry := slowRetryConfig{max: 3, base: time.Millisecond}
	if err := streamTableSlow(context.Background(), fq, table, dir, nil, retry, DefaultSlowChunkSize); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStreamTableSlowRetryExhaustedPreservesCheckpoint(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "name", DataType: "text"},
		},
	}

	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	prior := `{"id":1,"name":"v1"}` + "\n"
	if err := os.WriteFile(tmpPath, []byte(prior), 0o644); err != nil {
		t.Fatal(err)
	}
	ckptPath := checkpointPath(dir, "users")
	ckpt := slowCheckpoint{Table: "users", PKColumns: []string{"id"}, LastPK: []any{json.Number("1")}}
	ckptData, _ := json.Marshal(ckpt)
	if err := os.WriteFile(ckptPath, ckptData, 0o644); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").
		WithArgs(int64(1)).
		WillReturnError(fmt.Errorf("connection reset"))

	fq := &flakyQuerier{inner: sqlDB, failsRemaining: 1, failErr: fmt.Errorf("connection reset")}
	retry := slowRetryConfig{max: 1, base: time.Millisecond}
	err = streamTableSlow(context.Background(), fq, table, dir, nil, retry, DefaultSlowChunkSize)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "query table") {
		t.Fatalf("error missing query context: %v", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != prior {
		t.Fatalf("temp file changed after failed retry:\n%s", string(data))
	}
	if _, err := os.Stat(ckptPath); err != nil {
		t.Fatal("checkpoint should be preserved for resume")
	}
}

func TestStreamTableSlowRetryNoRetryOnCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
		},
	}

	retry := slowRetryConfig{max: 5, base: time.Millisecond}
	err = streamTableSlow(ctx, sqlDB, table, t.TempDir(), nil, retry, DefaultSlowChunkSize)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestStreamTableSlowRowsErrRetryable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := pkUsersTable()
	first := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2").
		RowError(1, &pgconn.PgError{Code: "40001"})
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").WillReturnRows(first)
	second := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		AddRow(2, "v2")
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").WillReturnRows(second)

	dir := t.TempDir()
	retry := slowRetryConfig{max: 3, base: time.Millisecond}
	if err := streamTableSlow(context.Background(), sqlDB, table, dir, nil, retry, DefaultSlowChunkSize); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tableDataPath(dir, table))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 without duplicates: %s", len(lines), data)
	}
}

func TestStreamTableSlowRowsErrNonRetryable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := pkUsersTable()
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "v1").
		RowError(0, &pgconn.PgError{Code: "22012"})
	mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").WillReturnRows(rows)

	dir := t.TempDir()
	err = streamTableSlow(context.Background(), sqlDB, table, dir, nil, slowRetryConfig{max: 3, base: time.Millisecond}, DefaultSlowChunkSize)
	if err == nil || !strings.Contains(err.Error(), "iterate rows") {
		t.Fatalf("error = %v, want iteration failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tableDataPath(dir, table)); !os.IsNotExist(err) {
		t.Fatal("final data file must not be published")
	}
}

func TestStreamTableSlowRowsErrExhaustion(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := pkUsersTable()
	for range 2 {
		rows := sqlmock.NewRows([]string{"id", "name"}).
			AddRow(1, "v1").
			RowError(0, &pgconn.PgError{Code: "40001"})
		mock.ExpectQuery("SELECT .* FROM .* ORDER BY .* LIMIT 1000").WillReturnRows(rows)
	}

	dir := t.TempDir()
	err = streamTableSlow(context.Background(), sqlDB, table, dir, nil, slowRetryConfig{max: 1, base: time.Millisecond}, DefaultSlowChunkSize)
	if err == nil || !strings.Contains(err.Error(), "iterate rows") {
		t.Fatalf("error = %v, want exhausted iteration failure", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tableDataPath(dir, table)); !os.IsNotExist(err) {
		t.Fatal("final data file must not be published")
	}
}

func pkUsersTable() db.Table {
	return db.Table{Schema: "public", Name: "users", Columns: []db.Column{
		{Name: "id", DataType: "integer", PrimaryKey: true}, {Name: "name", DataType: "text"},
	}}
}

func TestCheckpointFormatDiscrimination(t *testing.T) {
	table := pkUsersTable()
	desc := SelectKeyDescriptor(table)
	gen := func(lastKey []any) slowCheckpoint {
		return slowCheckpoint{Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: lastKey}
	}

	validateCases := []struct {
		cp      slowCheckpoint
		wantErr string
	}{
		{gen([]any{json.Number("1")}), ""},
		{slowCheckpoint{PKColumns: []string{"id"}, LastPK: []any{json.Number("1")}}, ""},
		{slowCheckpoint{Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: "deadbeef", LastKey: []any{json.Number("1")}}, "fingerprint mismatch"},
		{slowCheckpoint{Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"other"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "key columns mismatch"},
		{slowCheckpoint{PKColumns: []string{"other"}, LastPK: []any{json.Number("1")}}, "pk columns mismatch"},
		{slowCheckpoint{PKColumn: "id"}, ""},
		{slowCheckpoint{Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, LastKey: []any{json.Number("1")}}, "fingerprint required"},
		{slowCheckpoint{KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "strategy required"},
		{gen([]any{json.Number("1")}).withLegacy(), "mixes generalized and legacy"},
		{slowCheckpoint{PKColumn: "id", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "mixes legacy single-pk"},
		{slowCheckpoint{Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id", "code"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "key arity mismatch"},
		{slowCheckpoint{Strategy: "bogus", KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "unknown strategy"},
	}
	for i, tt := range validateCases {
		name := tt.wantErr
		if name == "" {
			name = fmt.Sprintf("ok%d", i)
		}
		t.Run(name, func(t *testing.T) {
			err := validateCheckpointDescriptor(&tt.cp, desc)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	dir := t.TempDir()
	path := checkpointPath(dir, "users")
	if err := saveSlowCheckpoint(path, table, desc, []any{int64(42)}); err != nil {
		t.Fatal(err)
	}
	cp, err := loadSlowCheckpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Strategy != KeyStrategyPrimaryKey || !slices.Equal(cp.KeyColumns, []string{"id"}) ||
		cp.KeyFingerprint != desc.Fingerprint || len(cp.LastKey) != 1 {
		t.Fatalf("generalized save: strategy=%q cols=%v fp=%q last=%v", cp.Strategy, cp.KeyColumns, cp.KeyFingerprint, cp.LastKey)
	}
	if n, ok := cp.LastKey[0].(json.Number); !ok || n.String() != "42" {
		t.Fatalf("generalized save LastKey = %v", cp.LastKey)
	}
	saveData, _ := os.ReadFile(path)
	if strings.Contains(string(saveData), `"last_pk"`) {
		t.Fatalf("generalized save must not use last_pk: %s", saveData)
	}

	uidesc := KeyDescriptor{Strategy: KeyStrategyUniqueIndex, Columns: []KeyColumn{{Name: "code"}}, Fingerprint: "abc"}
	if err := validateCheckpointDescriptor(&slowCheckpoint{PKColumns: []string{"id"}, LastPK: []any{json.Number("1")}}, uidesc); err == nil ||
		!strings.Contains(err.Error(), "legacy checkpoint incompatible") {
		t.Fatalf("legacy vs non-PK descriptor: err=%v", err)
	}

	legacy, err := loadSlowCheckpointFromData([]byte(`{"table":"users","pk_columns":["id"],"last_pk":[99]}`))
	if err != nil || !slices.Equal(legacy.PKColumns, []string{"id"}) || len(legacy.LastPK) != 1 {
		t.Fatalf("legacy load: cp=%+v err=%v", legacy, err)
	}

	tmpPath := filepath.Join(dir, "users.ndjson.tmp")
	if err := os.WriteFile(tmpPath, []byte(`{"id":7,"name":"seven"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSlowCheckpointTemp(tmpPath, &slowCheckpoint{Table: "users", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, LastKey: []any{json.Number("7")}}); err != nil {
		t.Fatal(err)
	}

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	resumeDir := t.TempDir()
	resumeTmp := filepath.Join(resumeDir, "users.ndjson.tmp")
	if err := os.WriteFile(resumeTmp, []byte(`{"id":10,"name":"ten"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumeCkpt, _ := json.Marshal(gen([]any{json.Number("10")}))
	if err := os.WriteFile(checkpointPath(resumeDir, "users"), resumeCkpt, 0o644); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* FROM .* WHERE .* > .* ORDER BY .* LIMIT 1000").WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(11, "eleven"))
	if err := streamTableSlowDefault(context.Background(), sqlDB, table, resumeDir, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(resumeDir, "users.ndjson"))
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != 2 || got[1] != `{"id":11,"name":"eleven"}` {
		t.Fatalf("resume output = %s", data)
	}

	mismatchDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mismatchDir, "users.ndjson.tmp"), []byte(`{"id":1,"name":"v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	streamReject := func(dir string, cp slowCheckpoint, want string) {
		t.Helper()
		b, _ := json.Marshal(cp)
		if err := os.WriteFile(checkpointPath(dir, "users"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		err := streamTableSlowDefault(context.Background(), sqlDB, table, dir, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("stream %q: err=%v", want, err)
		}
	}
	streamReject(mismatchDir, slowCheckpoint{Table: "users", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: "mismatch", LastKey: []any{json.Number("1")}}, "fingerprint mismatch")
	arityDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(arityDir, "users.ndjson.tmp"), []byte(`{"id":1,"name":"v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	streamReject(arityDir, slowCheckpoint{Table: "users", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1"), json.Number("2")}}, "key arity mismatch")
	colDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(colDir, "users.ndjson.tmp"), []byte(`{"id":1,"name":"v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	streamReject(colDir, slowCheckpoint{Table: "users", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"other"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "key columns mismatch")
	mixDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mixDir, "users.ndjson.tmp"), []byte(`{"id":1,"name":"v1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	streamReject(mixDir, slowCheckpoint{Table: "users", PKColumn: "id", Strategy: KeyStrategyPrimaryKey, KeyColumns: []string{"id"}, KeyFingerprint: desc.Fingerprint, LastKey: []any{json.Number("1")}}, "mixes legacy single-pk")
}

func (cp slowCheckpoint) withLegacy() slowCheckpoint {
	cp.PKColumns = []string{"id"}
	cp.LastPK = []any{json.Number("1")}
	return cp
}

func loadSlowCheckpointFromData(data []byte) (*slowCheckpoint, error) {
	dir, err := os.MkdirTemp("", "ckpt-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "test.ckpt.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return loadSlowCheckpoint(path)
}
