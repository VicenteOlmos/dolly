package clone

import (
	"context"
	"fmt"
	"time"
)

// TemplateStrategy performs same-instance CREATE DATABASE ... WITH TEMPLATE.
type TemplateStrategy struct{}

func (s *TemplateStrategy) Name() string { return "template" }

func (s *TemplateStrategy) Execute(ctx context.Context, opts Options) error {
	if opts.SourceDSN == "" {
		return fmt.Errorf("source DSN is required")
	}
	if opts.CloneName == "" {
		return fmt.Errorf("clone name is required")
	}

	startedAt := time.Now()

	reportProgressEvent(opts, ProgressEvent{
		Phase:   "creating_from_template",
		Step:    "creating from template",
		Current: 1,
		Total:   1,
		Elapsed: time.Since(startedAt),
	})

	targetDSN := opts.TargetDSN
	var err error
	if targetDSN == "" {
		targetDSN, err = RewriteDSN(opts.SourceDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	} else {
		targetDSN, err = RewriteDSN(targetDSN, opts.CloneName)
		if err != nil {
			return fmt.Errorf("build target DSN: %w", err)
		}
	}

	same, err := SameInstance(opts.SourceDSN, targetDSN)
	if err != nil {
		return fmt.Errorf("check same instance: %w", err)
	}
	if !same {
		return fmt.Errorf("template strategy requires source and target to be on the same PostgreSQL instance; use schema-replay or logical-stream for cross-server clones")
	}

	sourceDB, err := ParseDBName(opts.SourceDSN)
	if err != nil {
		return fmt.Errorf("parse source database name: %w", err)
	}

	adminDSN, err := RewriteDSN(targetDSN, "postgres")
	if err != nil {
		return fmt.Errorf("build admin DSN: %w", err)
	}

	dbConn, err := sqlOpenDB(adminDSN)
	if err != nil {
		return fmt.Errorf("open admin connection: %w", err)
	}
	defer dbConn.Close()

	// Precondition: no active connections on the source database.
	var activeConns int
	queryErr := dbConn.QueryRowContext(ctx, `
		SELECT count(*) FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, sourceDB).Scan(&activeConns)
	if queryErr != nil {
		return fmt.Errorf("check active connections: %w", queryErr)
	}
	if activeConns > 0 {
		return fmt.Errorf("source database %q has %d active connection(s); terminate them with SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %q AND pid <> pg_backend_pid()", sourceDB, activeConns, sourceDB)
	}

	_, err = dbConn.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %s WITH TEMPLATE %s`, quoteIdentifier(opts.CloneName), quoteIdentifier(sourceDB)))
	if err != nil {
		return fmt.Errorf("create database from template: %w (hint: if the error mentions other users accessing the database, terminate active connections with SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %q AND pid <> pg_backend_pid())", err, sourceDB)
	}

	return nil
}

// Ensure TemplateStrategy implements Strategy.
var _ Strategy = (*TemplateStrategy)(nil)
