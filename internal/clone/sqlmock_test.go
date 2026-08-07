package clone

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgTextArrayConverter lets sqlmock match []string scope arguments passed to pgx-style queries.
type pgTextArrayConverter struct{}

func (pgTextArrayConverter) ConvertValue(v interface{}) (driver.Value, error) {
	if s, ok := v.([]string); ok {
		return s, nil
	}
	return driver.DefaultParameterConverter.ConvertValue(v)
}

func newSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(
		sqlmock.MonitorPingsOption(true),
		sqlmock.ValueConverterOption(pgTextArrayConverter{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func expectTerminateBackends(mock sqlmock.Sqlmock, dbName string) {
	mock.ExpectQuery(`SELECT pg_terminate_backend\(pid\) FROM pg_stat_activity WHERE datname = \$1 AND pid <> pg_backend_pid\(\)`).
		WithArgs(dbName).
		WillReturnRows(sqlmock.NewRows([]string{"pg_terminate_backend"}).AddRow(true))
}

func expectDropDatabase(mock sqlmock.Sqlmock, dbName string) *sqlmock.ExpectedExec {
	return mock.ExpectExec(`DROP DATABASE IF EXISTS ` + quoteIdentifier(dbName))
}

func withDropDatabaseSQLMock(t *testing.T, fn func(sqlmock.Sqlmock)) {
	t.Helper()
	db, mock := newSQLMock(t)
	defer db.Close()
	origOpen := sqlOpenDB
	sqlOpenDB = func(string) (*sql.DB, error) { return db, nil }
	defer func() { sqlOpenDB = origOpen }()
	fn(mock)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDropDatabaseTerminatesThenRetriesOnActiveSessions(t *testing.T) {
	const name = "clone_target"
	withDropDatabaseSQLMock(t, func(mock sqlmock.Sqlmock) {
		expectTerminateBackends(mock, name)
		expectDropDatabase(mock, name).WillReturnError(&pgconn.PgError{Code: "55006"})
		expectTerminateBackends(mock, name)
		expectDropDatabase(mock, name).WillReturnResult(sqlmock.NewResult(0, 0))
		if err := dropDatabase(context.Background(), "postgres://admin/postgres", name); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDropDatabaseTerminationFailureStillAttemptsDrop(t *testing.T) {
	const name = "clone_target"
	withDropDatabaseSQLMock(t, func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery(`SELECT pg_terminate_backend\(pid\) FROM pg_stat_activity WHERE datname = \$1 AND pid <> pg_backend_pid\(\)`).
			WithArgs(name).
			WillReturnError(&pgconn.PgError{Code: "42501"})
		expectDropDatabase(mock, name).WillReturnResult(sqlmock.NewResult(0, 0))
		if err := dropDatabase(context.Background(), "postgres://admin/postgres", name); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDropDatabaseScopesAndQuotesName(t *testing.T) {
	const name = `db"quoted`
	withDropDatabaseSQLMock(t, func(mock sqlmock.Sqlmock) {
		expectTerminateBackends(mock, name)
		expectDropDatabase(mock, name).WillReturnResult(sqlmock.NewResult(0, 0))
		if err := dropDatabase(context.Background(), "postgres://admin/postgres", name); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDropDatabaseExhaustsActiveSessionRetries(t *testing.T) {
	const name = "clone_target"
	withDropDatabaseSQLMock(t, func(mock sqlmock.Sqlmock) {
		for range cleanupDropMaxAttempts {
			expectTerminateBackends(mock, name)
			expectDropDatabase(mock, name).WillReturnError(&pgconn.PgError{Code: "55006"})
		}
		err := dropDatabase(context.Background(), "postgres://admin/postgres", name)
		if err == nil || !strings.Contains(err.Error(), "55006") {
			t.Fatalf("error = %v, want 55006", err)
		}
	})
}

func TestDropDatabaseHonorsCanceledContextBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	origOpen := sqlOpenDB
	sqlOpenDB = func(string) (*sql.DB, error) {
		t.Fatal("sqlOpenDB called for canceled context")
		return nil, nil
	}
	defer func() { sqlOpenDB = origOpen }()
	if err := dropDatabase(ctx, "postgres://admin/postgres", "clone"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
