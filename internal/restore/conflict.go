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
		if policy == ConflictSkip || policy == ConflictUpsert {
			return "", nil, conflictRequiresKeyError(table, policy)
		}
		return fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", pgx.Identifier{table.Schema, table.Name}.Sanitize()), nil, nil
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
	valuesClause := strings.Join(placeholders, ", ")
	pkCols := primaryKeyColumns(table.Columns)
	conflictCols := conflictKeyColumns(table, pkCols)
	if hasAlwaysIdentity(table.Columns) {
		base := fmt.Sprintf(
			"INSERT INTO %s (%s) OVERRIDING SYSTEM VALUE VALUES (%s)",
			tableIdent,
			strings.Join(idents, ", "),
			valuesClause,
		)
		return finishInsert(base, table, policy, conflictCols, colNames)
	}
	base := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableIdent,
		strings.Join(idents, ", "),
		valuesClause,
	)
	return finishInsert(base, table, policy, conflictCols, colNames)
}

func conflictKeyColumns(table db.Table, pkCols []string) []string {
	if len(pkCols) > 0 {
		return pkCols
	}
	if len(table.UniqueKeys) > 0 {
		return table.UniqueKeys[0]
	}
	return nil
}

func conflictRequiresKeyError(table db.Table, policy ConflictPolicy) error {
	return fmt.Errorf(
		"conflict policy %s requires a primary key or unique key on %s.%s",
		policy.String(),
		table.Schema,
		table.Name,
	)
}

func hasAlwaysIdentity(cols []db.Column) bool {
	for _, c := range cols {
		if c.Identity == "ALWAYS" {
			return true
		}
	}
	return false
}

func finishInsert(base string, table db.Table, policy ConflictPolicy, conflictCols []string, colNames []string) (string, []string, error) {
	if policy == ConflictError {
		return base, colNames, nil
	}
	if len(conflictCols) == 0 {
		return "", nil, conflictRequiresKeyError(table, policy)
	}

	keyIdents := make([]string, len(conflictCols))
	for i, name := range conflictCols {
		keyIdents[i] = pgx.Identifier{name}.Sanitize()
	}
	conflictTarget := strings.Join(keyIdents, ", ")

	switch policy {
	case ConflictSkip:
		return base + fmt.Sprintf(" ON CONFLICT (%s) DO NOTHING", conflictTarget), colNames, nil
	case ConflictUpsert:
		var sets []string
		keySet := make(map[string]struct{}, len(conflictCols))
		for _, name := range conflictCols {
			keySet[name] = struct{}{}
		}
		for _, c := range table.Columns {
			if _, inKey := keySet[c.Name]; inKey {
				continue
			}
			if c.Identity == "ALWAYS" || c.Generated {
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
	idents := make([]string, len(tables))
	for i, table := range tables {
		idents[i] = pgx.Identifier{table.Schema, table.Name}.Sanitize()
	}
	if len(idents) > 0 {
		if _, err := q.ExecContext(ctx, "TRUNCATE TABLE "+strings.Join(idents, ", ")); err != nil {
			return fmt.Errorf("truncate tables: %w", err)
		}
	}
	return nil
}
