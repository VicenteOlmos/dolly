package dump

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func streamTableSlowDefault(ctx context.Context, q querier, table db.Table, dir string, rowTransform RowTransform) error {
	return streamTableSlow(ctx, q, table, dir, rowTransform, slowRetryConfig{}, DefaultSlowChunkSize)
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
	if !strings.Contains(err.Error(), "no primary key") {
		t.Fatalf("error missing no-PK context: %v", err)
	}
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
	if !strings.Contains(string(ckptData), fmt.Sprintf(`"last_pk":[%d]`, bigID)) {
		t.Fatalf("checkpoint did not store exact bigint: %s", string(ckptData))
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
