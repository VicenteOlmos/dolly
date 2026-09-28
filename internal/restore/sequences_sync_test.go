package restore

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestSyncSequencesToDataQuotesQualifiedMixedCaseTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tables := []db.Table{{Schema: "App", Name: "UserTable", Columns: []db.Column{{Name: "UserID"}}}}
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).WithArgs("App", "UserTable").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).AddRow("App", "UserTable", "UserID"))
	mock.ExpectExec(`pg_get_serial_sequence\('"App"\."UserTable"', 'UserID'\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := SyncSequencesToData(context.Background(), sqlDB, tables); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataPreservesEmptyTableState(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tables := []db.Table{{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id"}}}}
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).WithArgs("public", "users").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).AddRow("public", "users", "id"))
	mock.ExpectExec(`CASE WHEN m\.max_value IS NULL THEN NULL ELSE setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := SyncSequencesToData(context.Background(), sqlDB, tables); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataEmptyTablesReturns(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	_ = mock
	defer sqlDB.Close()
	if err := SyncSequencesToData(context.Background(), sqlDB, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataSkipsSerialOnAbsentTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tables := []db.Table{{Schema: "public", Name: "users", Columns: []db.Column{{Name: "id"}}}}
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public", "users").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "users", "id").
			AddRow("public", "orders", "id"))
	mock.ExpectExec(`pg_get_serial_sequence\('"public"\."users"', 'id'\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := SyncSequencesToData(context.Background(), sqlDB, tables); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
