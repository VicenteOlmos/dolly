package dbanalyze

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func expectObjectsQuery(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(`SELECT n\.nspname`).WillReturnRows(rows)
}

func expectObjectsQuerySchemas(mock sqlmock.Sqlmock, schemas []string, rows *sqlmock.Rows) {
	args := make([]driver.Value, len(schemas))
	for i, s := range schemas {
		args[i] = s
	}
	mock.ExpectQuery(`SELECT n\.nspname`).
		WithArgs(args...).
		WillReturnRows(rows)
}

func TestAnalyzeSourceEmptyDB(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}))
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(0))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(sqlmock.NewRows([]string{"datname"}))

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TableCount != 0 {
		t.Fatalf("TableCount = %d, want 0", result.TableCount)
	}
	if len(result.Objects) != 0 {
		t.Fatalf("Objects len = %d, want 0", len(result.Objects))
	}
	if result.NextCloneName != "app_dolly_1" {
		t.Fatalf("NextCloneName = %q, want app_dolly_1", result.NextCloneName)
	}
}

func TestAnalyzeSourcePerObjectStats(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuerySchemas(mock, []string{"public", "app"},
		sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}).
			AddRow("public", "users", "table", int64(1200), int64(1024*1024)).
			AddRow("app", "orders", "table", int64(50000), int64(8*1024*1024)),
	)
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(1024 * 1024 * 50))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(sqlmock.NewRows([]string{"datname"}))

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", []string{"public", "app"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Objects) != 2 {
		t.Fatalf("Objects len = %d, want 2", len(result.Objects))
	}
	if result.Objects[0].Name != "users" || result.Objects[0].RowEstimate != 1200 {
		t.Fatalf("Objects[0] = %+v, want users/1200", result.Objects[0])
	}
	if result.TotalRowEstimate != 51200 {
		t.Fatalf("TotalRowEstimate = %d, want 51200", result.TotalRowEstimate)
	}
}

func TestAnalyzeSourcePopulatedDB(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock,
		sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}).
			AddRow("public", "t1", "table", int64(1), int64(100)).
			AddRow("public", "t2", "table", int64(2), int64(200)).
			AddRow("public", "t3", "table", int64(3), int64(300)).
			AddRow("public", "t4", "table", int64(4), int64(400)).
			AddRow("public", "t5", "table", int64(5), int64(500)),
	)
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(1024 * 1024 * 50))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(sqlmock.NewRows([]string{"datname"}))

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TableCount != 5 {
		t.Fatalf("TableCount = %d, want 5", result.TableCount)
	}
	if result.DatabaseSize != 1024*1024*50 {
		t.Fatalf("DatabaseSize = %d, want %d", result.DatabaseSize, 1024*1024*50)
	}
}

func TestAnalyzeSourceNameCollision(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}).
		AddRow("public", "t1", "table", int64(1), int64(100)).
		AddRow("public", "t2", "table", int64(2), int64(200)).
		AddRow("public", "t3", "table", int64(3), int64(300)),
	)
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(100))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(
		sqlmock.NewRows([]string{"datname"}).
			AddRow("app_dolly_1").
			AddRow("app_dolly_2"),
	)

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCloneName != "app_dolly_3" {
		t.Fatalf("NextCloneName = %q, want app_dolly_3", result.NextCloneName)
	}
}

func TestAnalyzeSourceCtxCancel(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock.ExpectQuery(`SELECT n\.nspname`).WillReturnError(context.Canceled)

	_, err := AnalyzeSource(ctx, db, "app", "{db}_dolly_{n}", nil)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestAnalyzeSourceNilDB(t *testing.T) {
	_, err := AnalyzeSource(context.Background(), nil, "app", "{db}_dolly_{n}", nil)
	if err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestAnalyzeSourceDefaultTemplate(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}))
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(0))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(sqlmock.NewRows([]string{"datname"}))

	result, err := AnalyzeSource(context.Background(), db, "app", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCloneName != "app_dolly_1" {
		t.Fatalf("NextCloneName = %q, want app_dolly_1", result.NextCloneName)
	}
}

func TestAnalyzeSourceNonSequentialCollision(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}).
		AddRow("public", "t1", "table", int64(1), int64(100)).
		AddRow("public", "t2", "table", int64(2), int64(200)),
	)
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(100))
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(
		sqlmock.NewRows([]string{"datname"}).
			AddRow("app_dolly_1").
			AddRow("app_dolly_3"),
	)

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCloneName != "app_dolly_2" {
		t.Fatalf("NextCloneName = %q, want app_dolly_2", result.NextCloneName)
	}
}

func TestAnalyzeSourceManyCollisions(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}).
		AddRow("public", "t1", "table", int64(1), int64(100)))
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(100))

	rows := sqlmock.NewRows([]string{"datname"})
	for i := 1; i <= 10; i++ {
		rows.AddRow(fmt.Sprintf("app_dolly_%d", i))
	}
	mock.ExpectQuery(`SELECT datname FROM pg_database`).WillReturnRows(rows)

	result, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NextCloneName != "app_dolly_11" {
		t.Fatalf("NextCloneName = %q, want app_dolly_11", result.NextCloneName)
	}
}

func TestAnalyzeSourceObjectsError(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	mock.ExpectQuery(`SELECT n\.nspname`).WillReturnError(fmt.Errorf("db error"))

	_, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err == nil {
		t.Fatal("expected error for objects failure")
	}
	if !strings.Contains(err.Error(), "analyze objects") {
		t.Fatalf("error = %q, want 'analyze objects'", err.Error())
	}
}

func TestAnalyzeSourceSizeError(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}))
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnError(fmt.Errorf("size error"))

	_, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err == nil {
		t.Fatal("expected error for size query failure")
	}
	if !strings.Contains(err.Error(), "analyze database size") {
		t.Fatalf("error = %q, want 'analyze database size'", err.Error())
	}
}

func TestAnalyzeSourceNameProbeLikeArg(t *testing.T) {
	db, mock := newSQLMock(t)
	defer db.Close()

	expectObjectsQuery(mock, sqlmock.NewRows([]string{"schema", "name", "kind", "rows", "size"}))
	mock.ExpectQuery(`SELECT pg_database_size`).WillReturnRows(sqlmock.NewRows([]string{"size"}).AddRow(0))
	mock.ExpectQuery(`SELECT datname FROM pg_database WHERE datname LIKE \$1`).
		WithArgs(`app\_dolly\_%`).
		WillReturnRows(sqlmock.NewRows([]string{"datname"}))

	_, err := AnalyzeSource(context.Background(), db, "app", "{db}_dolly_{n}", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
