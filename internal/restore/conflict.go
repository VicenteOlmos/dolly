package restore

import (
	"context"
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
)

// ConflictPolicy controls row-level insert behavior.
type ConflictPolicy int

const (
	ConflictError ConflictPolicy = iota
	ConflictSkip
	ConflictUpsert
)

func (p ConflictPolicy) String() string {
	switch p {
	case ConflictError:
		return "error"
	case ConflictSkip:
		return "skip"
	case ConflictUpsert:
		return "upsert"
	default:
		return "unknown"
	}
}

// ParseConflictPolicy parses a CLI policy name.
func ParseConflictPolicy(s string) (ConflictPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "error":
		return ConflictError, nil
	case "skip":
		return ConflictSkip, nil
	case "upsert":
		return ConflictUpsert, nil
	default:
		return ConflictError, fmt.Errorf("unknown conflict policy %q", s)
	}
}

func buildInsert(table db.Table, policy ConflictPolicy) (query string, colNames []string, err error) {
	if len(table.Columns) == 0 {
		return "", nil, fmt.Errorf("table %q has no columns", table.Name)
	}

	colNames = make([]string, len(table.Columns))
	placeholders := make([]string, len(table.Columns))
	idents := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		colNames[i] = c.Name
		idents[i] = pgx.Identifier{c.Name}.Sanitize()
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	base := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableIdent,
		strings.Join(idents, ", "),
		strings.Join(placeholders, ", "),
	)

	pkCols := primaryKeyColumns(table.Columns)
	if len(pkCols) == 0 || policy == ConflictError {
		return base, colNames, nil
	}

	pkIdents := make([]string, len(pkCols))
	for i, name := range pkCols {
		pkIdents[i] = pgx.Identifier{name}.Sanitize()
	}
	conflictTarget := strings.Join(pkIdents, ", ")

	switch policy {
	case ConflictSkip:
		return base + fmt.Sprintf(" ON CONFLICT (%s) DO NOTHING", conflictTarget), colNames, nil
	case ConflictUpsert:
		var sets []string
		pkSet := make(map[string]struct{}, len(pkCols))
		for _, name := range pkCols {
			pkSet[name] = struct{}{}
		}
		for _, c := range table.Columns {
			if _, isPK := pkSet[c.Name]; isPK {
				continue
			}
			ident := pgx.Identifier{c.Name}.Sanitize()
			sets = append(sets, fmt.Sprintf("%s = EXCLUDED.%s", ident, ident))
		}
		if len(sets) == 0 {
			return base + fmt.Sprintf(" ON CONFLICT (%s) DO NOTHING", conflictTarget), colNames, nil
		}
		return base + fmt.Sprintf(
			" ON CONFLICT (%s) DO UPDATE SET %s",
			conflictTarget,
			strings.Join(sets, ", "),
		), colNames, nil
	default:
		return base, colNames, nil
	}
}

func primaryKeyColumns(cols []db.Column) []string {
	var pk []string
	for _, c := range cols {
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}
	return pk
}

// canUseCopy reports whether COPY is usable for the given policy.
// COPY is an atomic pgx bulk-transfer that does NOT support ON CONFLICT clauses,
// so it only works when no conflict handling is needed: error-conflict or replace.
func canUseCopy(policy ConflictPolicy) bool {
	return policy == ConflictError
}

func truncateTables(ctx context.Context, q execQuerier, tables []db.Table) error {
	for i := len(tables) - 1; i >= 0; i-- {
		table := tables[i]
		ident := pgx.Identifier{table.Schema, table.Name}.Sanitize()
		stmt := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", ident)
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("truncate table %q: %w", table.Name, err)
		}
	}
	return nil
}
