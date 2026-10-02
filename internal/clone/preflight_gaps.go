package clone

import (
	"context"
	"database/sql"
	"fmt"
)

// SchemaReplayGapCounts tallies source objects that catalog replay still cannot finish alone.
type SchemaReplayGapCounts struct {
	ForeignTables int
}

// SchemaReplayGapWarnings formats non-zero gap counts as preflight warning lines.
func SchemaReplayGapWarnings(c SchemaReplayGapCounts) []string {
	var out []string
	if c.ForeignTables > 0 {
		out = append(out, fmt.Sprintf(
			"schema-replay will recreate %d foreign table(s); their foreign servers must already exist on the target",
			c.ForeignTables,
		))
	}
	return out
}

func scanSchemaReplayGapCounts(ctx context.Context, dbConn *sql.DB, scope []string) (SchemaReplayGapCounts, error) {
	var out SchemaReplayGapCounts
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)

	if err := scanGapCount(ctx, dbConn, `
		SELECT COUNT(*)::bigint
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'f'
		`+userSchemaFilter+userRelationFilter+scopePred, scopeArgs, &out.ForeignTables); err != nil {
		return SchemaReplayGapCounts{}, fmt.Errorf("count foreign tables: %w", err)
	}
	return out, nil
}

func scanGapCount(ctx context.Context, dbConn *sql.DB, query string, args []any, dest *int) error {
	var n int64
	if err := queryRowContextOptional(ctx, dbConn, query, args).Scan(&n); err != nil {
		return err
	}
	*dest = int(n)
	return nil
}
