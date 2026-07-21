package restore

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func sequenceMetadata(schema, table, column, sequence string) dump.Metadata {
	return dump.Metadata{
		Tables:    []db.Table{{Schema: schema, Name: table, Columns: []db.Column{{Name: column}}}},
		Sequences: []dump.SequenceState{{Schema: schema, Name: sequence, StartValue: 5}},
	}
}

func expectSequenceOwner(mock sqlmock.Sqlmock, schema, table, column string) {
	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).
		WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}).AddRow(schema, table, column))
}

func TestRestoreSequencesFromMetadataRestoresOwnedSequence(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, false\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataRejectsUnownedMetadata(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "other_seq")
	expectSequenceOwner(mock, "private", "secrets", "id")
	err = RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil)
	if err == nil || !strings.Contains(err.Error(), "not owned by a restored column") {
		t.Fatalf("err = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataSkipsStandaloneSequence(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "other_seq")
	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataContinuesPastStandaloneSequence(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences = append([]dump.SequenceState{{Schema: "public", Name: "standalone_seq", StartValue: 1}}, meta.Sequences...)
	mock.ExpectQuery(`SELECT tbl_ns.nspname, tbl.relname, a.attname`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column"}))
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, false\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataScopesSchemas(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences = append(meta.Sequences, dump.SequenceState{Schema: "other", Name: "secret_seq", StartValue: 1})
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectExec(`SELECT setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, []string{"public"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataQuotesQualifiedMixedCaseTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).WithArgs("App").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).AddRow("App", "UserTable", "UserID"))
	mock.ExpectExec(`pg_get_serial_sequence\('"App"\."UserTable"', 'UserID'\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := SyncSequencesToData(context.Background(), sqlDB, []string{"App"}); err != nil {
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
	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).WithArgs("public").WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).AddRow("public", "users", "id"))
	mock.ExpectExec(`CASE WHEN m\.max_value IS NULL THEN NULL ELSE setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := SyncSequencesToData(context.Background(), sqlDB, []string{"public"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataEmptySchemasReturns(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := SyncSequencesToData(context.Background(), sqlDB, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
