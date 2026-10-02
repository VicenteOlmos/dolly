package clone

import (
	"context"
	"database/sql"
)

// SchemaReplayGapCounts is retained so preflight can grow new non-fatal warnings
// without another signature change. Catalog replay now copies the objects that
// used to be reported here.
type SchemaReplayGapCounts struct{}

// SchemaReplayGapWarnings formats non-zero gap counts as preflight warning lines.
func SchemaReplayGapWarnings(SchemaReplayGapCounts) []string {
	return nil
}

func scanSchemaReplayGapCounts(context.Context, *sql.DB, []string) (SchemaReplayGapCounts, error) {
	return SchemaReplayGapCounts{}, nil
}

func scanGapCount(ctx context.Context, dbConn *sql.DB, query string, args []any, dest *int) error {
	var n int64
	if err := queryRowContextOptional(ctx, dbConn, query, args).Scan(&n); err != nil {
		return err
	}
	*dest = int(n)
	return nil
}
