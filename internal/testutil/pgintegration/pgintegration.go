package pgintegration

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var fixtureMu sync.Mutex

// EnvDSN is the environment variable holding the PostgreSQL connection string for integration tests.
const EnvDSN = "DOLLY_TEST_PG_DSN"

//go:embed fixtures.sql
var fixturesSQL string

func pingDSN(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

// Open returns a database connection or skips the test when DOLLY_TEST_PG_DSN is unset or unreachable.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(EnvDSN)
	if dsn == "" {
		t.Skipf("set %s to run integration tests", EnvDSN)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pingDSN(ctx, dsn)
	if err != nil {
		t.Skipf("%s: database unreachable: %v", EnvDSN, err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

// SetupMainDB opens and bootstraps fixtures for TestMain. It returns nil when DSN is unset or unreachable.
func SetupMainDB() (*sql.DB, error) {
	dsn := os.Getenv(EnvDSN)
	if dsn == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pingDSN(ctx, dsn)
	if err != nil {
		return nil, nil
	}

	if err := Bootstrap(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

// Bootstrap applies fixture DDL and seed data without a testing.T (for TestMain).
func Bootstrap(ctx context.Context, db *sql.DB) error {
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	_, err := db.ExecContext(ctx, fixturesSQL)
	return err
}

// ApplyFixtures resets and seeds the integration schema in public.
func ApplyFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := Bootstrap(ctx, db); err != nil {
		t.Fatalf("apply fixtures: %v", err)
	}
}
