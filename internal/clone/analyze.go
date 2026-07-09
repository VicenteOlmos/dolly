package clone

import (
	"context"
	"database/sql"

	"github.com/VicenteOlmos/dolly/internal/dbanalyze"
)

// AnalyzeResult holds the output of an analyze preflight against the source database.
type AnalyzeResult = dbanalyze.AnalyzeResult

// AnalyzeSource queries the source database for table count, database size,
// and the next free clone name.
func AnalyzeSource(ctx context.Context, db *sql.DB, sourceDB, nameTpl string, schemas []string) (AnalyzeResult, error) {
	return dbanalyze.AnalyzeSource(ctx, db, sourceDB, nameTpl, schemas)
}

// analyzeSourceFunc is a test seam mirroring preflightFunc/dumpFunc.
var analyzeSourceFunc = AnalyzeSource
