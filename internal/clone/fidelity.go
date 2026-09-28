package clone

import (
	"context"
	"database/sql"
	"fmt"
)

// applyTargetFidelity disables user triggers, runs restore, re-enables triggers,
// and refreshes materialized views after a successful restore.
var applyTargetFidelity = applyTargetFidelityDefault

func applyTargetFidelityDefault(ctx context.Context, db *sql.DB, restore func() error) error {
	if err := setUserTriggers(ctx, db, false); err != nil {
		return fmt.Errorf("disable user triggers: %w", err)
	}
	restoreErr := restore()
	if err := setUserTriggers(ctx, db, true); err != nil {
		if restoreErr != nil {
			return fmt.Errorf("restore: %w (also re-enable user triggers: %v)", restoreErr, err)
		}
		return fmt.Errorf("re-enable user triggers: %w", err)
	}
	if restoreErr != nil {
		return fmt.Errorf("restore: %w", restoreErr)
	}
	if err := refreshMaterializedViews(ctx, db); err != nil {
		return fmt.Errorf("refresh materialized views: %w", err)
	}
	return nil
}

func setUserTriggers(ctx context.Context, db *sql.DB, enable bool) error {
	rows, err := db.QueryContext(ctx, `
		SELECT n.nspname, c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND EXISTS (
		    SELECT 1 FROM pg_trigger t
		    WHERE t.tgrelid = c.oid AND NOT t.tgisinternal
		  )
		ORDER BY n.nspname, c.relname`)
	if err != nil {
		return err
	}
	defer rows.Close()

	verb := "DISABLE"
	if enable {
		verb = "ENABLE"
	}
	var tables [][2]string
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return err
		}
		tables = append(tables, [2]string{schema, name})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, table := range tables {
		stmt := fmt.Sprintf("ALTER TABLE %s %s TRIGGER USER", quoteQualifiedTable(table[0], table[1]), verb)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s triggers on %s.%s: %w", verb, table[0], table[1], err)
		}
	}
	return nil
}

func refreshMaterializedViews(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT schemaname, matviewname
		FROM pg_matviews
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schemaname, matviewname`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var views [][2]string
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return err
		}
		views = append(views, [2]string{schema, name})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, view := range views {
		stmt := "REFRESH MATERIALIZED VIEW " + quoteQualifiedTable(view[0], view[1])
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s.%s: %w", view[0], view[1], err)
		}
	}
	return nil
}
