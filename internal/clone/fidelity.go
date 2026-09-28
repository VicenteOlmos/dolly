package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var applyTargetFidelity = applyTargetFidelityDefault

type triggerState struct {
	schema, table, name, mode string
}

func applyTargetFidelityDefault(ctx context.Context, source, target *sql.DB, restore func() error) error {
	triggers, err := userTriggerStates(ctx, target)
	if err != nil {
		return fmt.Errorf("list user triggers: %w", err)
	}
	// Record states before changing any trigger, so partial failures can be undone.
	var altered []triggerState
	cleanup := func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var errs []error
		for _, trigger := range altered {
			if err := setTriggerMode(cleanupCtx, target, trigger, trigger.mode); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	for _, trigger := range triggers {
		if trigger.mode == "D" {
			continue
		}
		if err := setTriggerMode(ctx, target, trigger, "DISABLE"); err != nil {
			return errors.Join(fmt.Errorf("disable user triggers: %w", err), cleanup())
		}
		altered = append(altered, trigger)
	}
	restoreErr := restore()
	if err := cleanup(); err != nil {
		return errors.Join(restoreErr, fmt.Errorf("restore user triggers: %w", err))
	}
	if restoreErr != nil {
		return fmt.Errorf("restore: %w", restoreErr)
	}
	if err := refreshMaterializedViews(ctx, source, target); err != nil {
		return fmt.Errorf("refresh materialized views: %w", err)
	}
	return nil
}

func userTriggerStates(ctx context.Context, db *sql.DB) ([]triggerState, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT n.nspname, c.relname, t.tgname, t.tgenabled
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND NOT t.tgisinternal
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY n.nspname, c.relname, t.tgname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []triggerState
	for rows.Next() {
		var s triggerState
		if err := rows.Scan(&s.schema, &s.table, &s.name, &s.mode); err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	return states, rows.Err()
}

func setTriggerMode(ctx context.Context, db *sql.DB, trigger triggerState, mode string) error {
	verb := map[string]string{"D": "DISABLE", "O": "ENABLE", "R": "ENABLE REPLICA", "A": "ENABLE ALWAYS", "DISABLE": "DISABLE"}[mode]
	if verb == "" {
		return fmt.Errorf("unknown trigger mode %q", mode)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s %s TRIGGER %s", quoteQualifiedTable(trigger.schema, trigger.table), verb, quoteIdentifier(trigger.name))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("%s.%s.%s: %w", trigger.schema, trigger.table, trigger.name, err)
	}
	return nil
}

func refreshMaterializedViews(ctx context.Context, source, target *sql.DB) error {
	rows, err := source.QueryContext(ctx, `
		SELECT n.nspname, c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'm' AND c.relispopulated
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	if err != nil {
		return err
	}
	var views [][2]string
	for rows.Next() {
		var view [2]string
		if err := rows.Scan(&view[0], &view[1]); err != nil {
			rows.Close()
			return err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// Schema filters may leave source views absent from the target.
	rows, err = target.QueryContext(ctx, `SELECT schemaname, matviewname FROM pg_matviews`)
	if err != nil {
		return err
	}
	available := make(map[[2]string]bool)
	for rows.Next() {
		var view [2]string
		if err := rows.Scan(&view[0], &view[1]); err != nil {
			rows.Close()
			return err
		}
		available[view] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// pg_rewrite dependencies describe the referenced materialized views of each view.
	rows, err = target.QueryContext(ctx, `
		SELECT dn.nspname, dependent.relname, rn.nspname, referenced.relname
		FROM pg_depend d
		JOIN pg_rewrite r ON r.oid = d.objid
		JOIN pg_class dependent ON dependent.oid = r.ev_class
		JOIN pg_namespace dn ON dn.oid = dependent.relnamespace
		JOIN pg_class referenced ON referenced.oid = d.refobjid
		JOIN pg_namespace rn ON rn.oid = referenced.relnamespace
		WHERE d.classid = 'pg_rewrite'::regclass AND d.refclassid = 'pg_class'::regclass
		  AND dependent.relkind = 'm' AND referenced.relkind = 'm'
		  AND dependent.oid <> referenced.oid`)
	if err != nil {
		return err
	}
	deps := make(map[[2]string][][2]string)
	for rows.Next() {
		var dependent, referenced [2]string
		if err := rows.Scan(&dependent[0], &dependent[1], &referenced[0], &referenced[1]); err != nil {
			rows.Close()
			return err
		}
		deps[dependent] = append(deps[dependent], referenced)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	populated := make(map[[2]string]bool, len(views))
	for _, view := range views {
		if available[view] {
			populated[view] = true
		}
	}
	visited := make(map[[2]string]bool)
	visiting := make(map[[2]string]bool)
	var refresh func([2]string) error
	refresh = func(view [2]string) error {
		if visited[view] {
			return nil
		}
		if visiting[view] {
			return fmt.Errorf("materialized view dependency cycle at %s.%s", view[0], view[1])
		}
		if !populated[view] {
			return fmt.Errorf("populated materialized view depends on unpopulated %s.%s", view[0], view[1])
		}
		visiting[view] = true
		for _, dep := range deps[view] {
			if err := refresh(dep); err != nil {
				return err
			}
		}
		stmt := "REFRESH MATERIALIZED VIEW " + quoteQualifiedTable(view[0], view[1])
		if _, err := target.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s.%s: %w", view[0], view[1], err)
		}
		visiting[view] = false
		visited[view] = true
		return nil
	}
	for _, view := range views {
		if !available[view] {
			continue
		}
		if err := refresh(view); err != nil {
			return err
		}
	}
	return nil
}
