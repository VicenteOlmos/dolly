package restore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

type benchExecQuerier struct{}

func (benchExecQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, fmt.Errorf("unexpected query")
}

func (benchExecQuerier) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return benchResult(0), nil
}

type benchResult int64

func (r benchResult) LastInsertId() (int64, error) { return 0, nil }
func (r benchResult) RowsAffected() (int64, error) { return int64(r), nil }

func benchmarkTable() db.Table {
	return db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
			{Name: "email", DataType: "text", OrdinalPosition: 2},
			{Name: "active", DataType: "boolean", OrdinalPosition: 3},
		},
	}
}

func writeBenchNDJSON(b *testing.B, rows int) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "users.ndjson")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < rows; i++ {
		if _, err := fmt.Fprintf(f, `{"id":%d,"email":"user%d@example.com","active":true}`+"\n", i+1, i+1); err != nil {
			b.Fatal(err)
		}
	}
	return path
}

func BenchmarkLoadTableInsertParsing(b *testing.B) {
	table := benchmarkTable()
	path := writeBenchNDJSON(b, 1000)
	q := benchExecQuerier{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := loadTable(ctx, q, table, path, ConflictError); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNDJSONCopySourceParsing(b *testing.B) {
	table := benchmarkTable()
	path := writeBenchNDJSON(b, 1000)
	colNames := columnNames(table.Columns)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src, err := newNDJSONCopySource(path, table, colNames)
		if err != nil {
			b.Fatal(err)
		}
		for src.Next() {
			if _, err := src.Values(); err != nil {
				b.Fatal(err)
			}
		}
		if err := src.Err(); err != nil {
			b.Fatal(err)
		}
		src.close()
	}
}
