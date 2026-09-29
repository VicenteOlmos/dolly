package clone

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/jackc/pgx/v5"
)

type memRows struct {
	rows [][]any
	i    int
}

func (m *memRows) Next() bool {
	if m.i >= len(m.rows) {
		return false
	}
	m.i++
	return true
}

func (m *memRows) Scan(dest ...any) error {
	row := m.rows[m.i-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d dests, row has %d", len(dest), len(row))
	}
	for i, d := range dest {
		ptr, ok := d.(*any)
		if !ok {
			return fmt.Errorf("dest %d is %T", i, d)
		}
		*ptr = row[i]
	}
	return nil
}

func (m *memRows) Err() error { return nil }
func (m *memRows) Close()     {}

type memCopy struct {
	rows   *memRows
	copied [][]any
}

func (m *memCopy) CopyTo(context.Context, io.Writer, string) error { return nil }
func (m *memCopy) CopyFrom(context.Context, io.Reader, string) error {
	return nil
}
func (m *memCopy) Close(context.Context) error { return nil }

func (m *memCopy) Query(context.Context, string) (copyQueryRows, error) {
	return m.rows, nil
}

func (m *memCopy) CopyFromSource(_ context.Context, _, _ string, _ []string, src pgx.CopyFromSource) (int64, error) {
	var n int64
	for src.Next() {
		vals, err := src.Values()
		if err != nil {
			return n, err
		}
		m.copied = append(m.copied, append([]any(nil), vals...))
		n++
	}
	return n, src.Err()
}

func TestCopyTableSanitizedRedactsEmail(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
			{Name: "email", DataType: "text"},
		},
	}
	src := &memCopy{rows: &memRows{rows: [][]any{{int64(1), "person@example.com"}}}}
	tgt := &memCopy{}
	if err := copyTableSanitized(context.Background(), src, tgt, table, dump.SanitizeByPattern); err != nil {
		t.Fatal(err)
	}
	if len(tgt.copied) != 1 || tgt.copied[0][0] != int64(1) || tgt.copied[0][1] != "redacted@example.com" {
		t.Fatalf("copied = %#v", tgt.copied)
	}
}
