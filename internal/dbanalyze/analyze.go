package dbanalyze

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ObjectStat holds per-relation stats from the analyze preflight.
type ObjectStat struct {
	Schema      string
	Name        string
	Kind        string // table, partition, view, matview
	RowEstimate int64
	SizeBytes   int64
}

// AnalyzeResult holds the output of an analyze preflight against the source database.
type AnalyzeResult struct {
	TableCount       int
	DatabaseSize     int64
	NextCloneName    string
	TotalRowEstimate int64
	Objects          []ObjectStat
	ComputedAt       time.Time
}

// AnalyzeSource queries the source database for per-object stats, database size,
// and the next free clone name. When schemas is non-empty, only objects in those
// schemas are included.
func AnalyzeSource(ctx context.Context, db *sql.DB, sourceDB, nameTpl string, schemas []string) (AnalyzeResult, error) {
	if db == nil {
		return AnalyzeResult{}, fmt.Errorf("analyze: database connection is nil")
	}

	result := AnalyzeResult{ComputedAt: time.Now()}

	objects, err := fetchObjects(ctx, db, schemas)
	if err != nil {
		return AnalyzeResult{}, err
	}
	result.Objects = objects
	result.TableCount = len(objects)
	for _, obj := range objects {
		result.TotalRowEstimate += obj.RowEstimate
	}

	var dbSize int64
	err = db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbSize)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("analyze database size: %w", err)
	}
	result.DatabaseSize = dbSize

	if nameTpl == "" {
		nameTpl = "{db}_dolly_{n}"
	}
	baseName := strings.ReplaceAll(nameTpl, "{db}", sourceDB)
	namePrefix := strings.ReplaceAll(baseName, "{n}", "")

	rows, err := db.QueryContext(ctx, `
		SELECT datname FROM pg_database WHERE datname LIKE $1
	`, likeEscape(namePrefix)+"%")
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("analyze name probe: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return AnalyzeResult{}, fmt.Errorf("analyze name scan: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return AnalyzeResult{}, fmt.Errorf("analyze name rows: %w", err)
	}

	n := 1
	for {
		candidate := strings.ReplaceAll(baseName, "{n}", fmt.Sprintf("%d", n))
		if !existing[candidate] {
			result.NextCloneName = candidate
			break
		}
		n++
		if n > 10000 {
			return AnalyzeResult{}, fmt.Errorf("analyze: could not find free clone name after %d attempts", n)
		}
	}

	return result, nil
}

const objectsQuery = `
	SELECT n.nspname,
	       c.relname,
	       CASE c.relkind
	         WHEN 'r' THEN 'table'
	         WHEN 'p' THEN 'partition'
	         WHEN 'v' THEN 'view'
	         WHEN 'm' THEN 'matview'
	         ELSE c.relkind::text
	       END,
	       COALESCE(GREATEST(s.n_live_tup, c.reltuples), 0)::bigint,
	       pg_total_relation_size(c.oid)
	FROM pg_class c
	INNER JOIN pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_stat_user_tables s ON s.schemaname = n.nspname AND s.relname = c.relname
	WHERE c.relkind IN ('r', 'p', 'v', 'm')
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg_temp_%'
	  AND n.nspname NOT LIKE 'pg_toast_%'
`

func fetchObjects(ctx context.Context, db *sql.DB, schemas []string) ([]ObjectStat, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if len(schemas) > 0 {
		placeholders := make([]string, len(schemas))
		args := make([]any, len(schemas))
		for i, schema := range schemas {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = schema
		}
		query := objectsQuery + fmt.Sprintf(`
	  AND n.nspname IN (%s)
	ORDER BY pg_total_relation_size(c.oid) DESC, n.nspname, c.relname`, strings.Join(placeholders, ", "))
		rows, err = db.QueryContext(ctx, query, args...)
	} else {
		query := objectsQuery + `
	ORDER BY pg_total_relation_size(c.oid) DESC, n.nspname, c.relname`
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("analyze objects: %w", err)
	}
	defer rows.Close()

	var objects []ObjectStat
	for rows.Next() {
		var obj ObjectStat
		if err := rows.Scan(&obj.Schema, &obj.Name, &obj.Kind, &obj.RowEstimate, &obj.SizeBytes); err != nil {
			return nil, fmt.Errorf("analyze objects scan: %w", err)
		}
		objects = append(objects, obj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analyze objects rows: %w", err)
	}
	return objects, nil
}

// likeEscape escapes the '%' and '_' characters in a LIKE pattern.
func likeEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 10)
	for _, r := range s {
		switch r {
		case '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
