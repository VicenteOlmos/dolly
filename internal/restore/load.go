package restore

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"database/sql"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func loadTable(ctx context.Context, q execQuerier, table db.Table, path string, policy ConflictPolicy) error {
	if err := dump.ValidateTableName(table.Name); err != nil {
		return fmt.Errorf("validate table: %w", err)
	}
	query, _, err := buildInsert(table, policy)
	if err != nil {
		return err
	}

	colNames := columnNames(table.Columns)

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open table %q data file: %w", table.Name, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxLine = 16 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLine)

	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return fmt.Errorf("table %q line %d: decode json: %w", table.Name, lineNo, err)
		}

		args, err := coerceRow(table.Columns, colNames, row)
		if err != nil {
			return fmt.Errorf("table %q line %d: %w", table.Name, lineNo, err)
		}

		if _, err := q.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("table %q line %d: insert: %w", table.Name, lineNo, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read table %q data file: %w", table.Name, err)
	}

	return nil
}

// loadTableCopy loads an NDJSON file into the target table using pgx COPY on a
// fresh connection. That session cannot see an uncommitted restore transaction.
// Conflict policies skip/upsert stay on loadTable because COPY has no ON CONFLICT.
//
// Transactional restore uses loadTableCopyInTx so COPY shares the sql.Tx session.
func loadTableCopy(ctx context.Context, dsn string, table db.Table, path string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("table %q: copy connect: %w", table.Name, err)
	}
	defer conn.Close(ctx)
	return copyFromPGX(ctx, conn, table, path)
}

// loadTableCopyInTx runs COPY on the pgx connection that owns an open sql transaction.
var loadTableCopyInTx = func(ctx context.Context, conn *sql.Conn, table db.Table, path string) error {
	return conn.Raw(func(driverConn any) error {
		std, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("table %q: transactional COPY requires the pgx database/sql driver", table.Name)
		}
		return copyFromPGX(ctx, std.Conn(), table, path)
	})
}

func copyFromPGX(ctx context.Context, conn *pgx.Conn, table db.Table, path string) error {
	if err := dump.ValidateTableName(table.Name); err != nil {
		return fmt.Errorf("validate table: %w", err)
	}
	if len(table.Columns) == 0 {
		return fmt.Errorf("table %q has no columns", table.Name)
	}

	colNames := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
	}

	colNameMap := columnNames(table.Columns)
	src, err := newNDJSONCopySource(path, table, colNameMap)
	if err != nil {
		return err
	}
	defer src.close()

	ident := pgx.Identifier{table.Schema, table.Name}
	_, err = conn.CopyFrom(ctx, ident, colNames, src)
	if err != nil {
		return fmt.Errorf("table %q: copy: %w", table.Name, err)
	}
	return src.err
}

// ndjsonCopySource reads NDJSON line-by-line and yields coerced rows
// for pgx.CopyFrom. It implements pgx.CopyFromSource.
type ndjsonCopySource struct {
	scanner  *bufio.Scanner
	file     *os.File
	columns  []db.Column
	colNames map[string]bool
	table    db.Table
	row      []any
	err      error
}

func newNDJSONCopySource(path string, table db.Table, colNames map[string]bool) (*ndjsonCopySource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open table %q data file: %w", table.Name, err)
	}
	scanner := bufio.NewScanner(f)
	const maxLine = 16 * 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLine)
	return &ndjsonCopySource{
		file:     f,
		scanner:  scanner,
		columns:  table.Columns,
		colNames: colNames,
		table:    table,
	}, nil
}

func (s *ndjsonCopySource) Next() bool {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			s.err = fmt.Errorf("table %q: decode json: %w", s.table.Name, err)
			return false
		}
		args, err := coerceRow(s.columns, s.colNames, raw)
		if err != nil {
			s.err = fmt.Errorf("table %q: %w", s.table.Name, err)
			return false
		}
		s.row = args
		return true
	}
	if err := s.scanner.Err(); err != nil {
		s.err = fmt.Errorf("table %q: read: %w", s.table.Name, err)
	}
	return false
}

func (s *ndjsonCopySource) Values() ([]any, error) {
	return s.row, nil
}

func (s *ndjsonCopySource) Err() error {
	return s.err
}

func (s *ndjsonCopySource) close() {
	if s.file != nil {
		s.file.Close()
	}
}
