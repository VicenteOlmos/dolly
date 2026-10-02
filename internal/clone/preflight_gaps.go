package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func clusterPreflightWarnings(ctx context.Context, dbConn *sql.DB) ([]string, error) {
	var tablespaces int
	if err := scanGapCount(ctx, dbConn, `
		SELECT COUNT(*)::bigint
		FROM pg_tablespace
		WHERE spcname NOT IN ('pg_default', 'pg_global')`, nil, &tablespaces); err != nil {
		return nil, fmt.Errorf("count tablespaces: %w", err)
	}
	var roles int
	if err := scanGapCount(ctx, dbConn, `
		SELECT COUNT(*)::bigint
		FROM pg_roles
		WHERE oid >= 16384`, nil, &roles); err != nil {
		return nil, fmt.Errorf("count roles: %w", err)
	}
	var servers int
	if err := scanGapCount(ctx, dbConn, `
		SELECT COUNT(*)::bigint
		FROM pg_foreign_server`, nil, &servers); err != nil {
		return nil, fmt.Errorf("count foreign servers: %w", err)
	}
	var out []string
	if tablespaces > 0 {
		out = append(out, fmt.Sprintf(
			"schema-replay will create %d tablespace(s) when missing; the location directory must already exist on the target host",
			tablespaces,
		))
	}
	if roles > 0 {
		visible, err := catalogRelationVisible(ctx, dbConn, `SELECT 1 FROM pg_authid WHERE oid >= 16384 LIMIT 1`)
		if err != nil {
			return nil, fmt.Errorf("read role passwords: %w", err)
		}
		if !visible {
			out = append(out, "schema-replay cannot read role passwords; missing roles are created without a password")
		}
	}
	if servers > 0 {
		visible, err := catalogRelationVisible(ctx, dbConn, `SELECT 1 FROM pg_user_mapping LIMIT 1`)
		if err != nil {
			return nil, fmt.Errorf("read user mappings: %w", err)
		}
		if !visible {
			out = append(out, "schema-replay cannot read user mapping options; mappings are created without options")
		}
	}
	return out, nil
}

func catalogRelationVisible(ctx context.Context, dbConn *sql.DB, query string) (bool, error) {
	var one int
	err := dbConn.QueryRowContext(ctx, query).Scan(&one)
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if isInsufficientPrivilege(err) {
		return false, nil
	}
	return false, err
}

func scanGapCount(ctx context.Context, dbConn *sql.DB, query string, args []any, dest *int) error {
	var n int64
	if err := queryRowContextOptional(ctx, dbConn, query, args).Scan(&n); err != nil {
		return err
	}
	*dest = int(n)
	return nil
}
