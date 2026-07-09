package restore

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

func TestRestoreSequencesFromMetadataHappyPath(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	lastVal := int64(42)
	meta := dump.Metadata{Sequences: []dump.SequenceState{{Schema: "public", Name: "users_id_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true}}}

	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 42, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataEmptyReturns(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, dump.Metadata{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataPropagatesSetvalError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	lastVal := int64(42)
	meta := dump.Metadata{Sequences: []dump.SequenceState{{Schema: "public", Name: "users_id_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true}}}

	mock.ExpectExec(`SELECT setval`).WillReturnError(errors.New("boom"))

	err = RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRestoreSequencesFromMetadataQuotedIdentifiers(t *testing.T) {
	tests := []struct {
		name       string
		schemaName string
		seqName    string
		wantRegex  string
	}{
		{
			name:       "mixed case",
			schemaName: "App",
			seqName:    "UserIDSeq",
			wantRegex:  `SELECT setval\('"App"\."UserIDSeq"'::regclass, 7, true\)`,
		},
		{
			name:       "embedded quote",
			schemaName: `we"ird`,
			seqName:    `seq"name`,
			wantRegex:  `SELECT setval\('"we""ird"\."seq""name"'::regclass, 7, true\)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer sqlDB.Close()

			lastVal := int64(7)
			meta := dump.Metadata{Sequences: []dump.SequenceState{{Schema: tt.schemaName, Name: tt.seqName, LastValue: &lastVal, StartValue: 1, IsCalled: true}}}

			mock.ExpectExec(tt.wantRegex).WillReturnResult(sqlmock.NewResult(1, 1))

			if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestoreSequencesFromMetadataStartValueWhenNeverCalled(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	meta := dump.Metadata{Sequences: []dump.SequenceState{{Schema: "public", Name: "users_id_seq", StartValue: 5}}}

	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, false\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

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

	lastVal := int64(5)
	meta := dump.Metadata{Sequences: []dump.SequenceState{
		{Schema: "public", Name: "users_id_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true},
		{Schema: "other", Name: "secret_seq", LastValue: &lastVal, StartValue: 1, IsCalled: true},
	}}

	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, []string{"public"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSequencesToDataScopesSchemasAndIdentityColumns(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public", "app").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "users", "id").
			AddRow("app", "events", "event_id"))
	mock.ExpectExec(`SELECT setval\(pg_get_serial_sequence\('public\.users', 'id'\), COALESCE\(\(SELECT max\("id"\) FROM "public"\."users"\), 1\), true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`SELECT setval\(pg_get_serial_sequence\('app\.events', 'event_id'\), COALESCE\(\(SELECT max\("event_id"\) FROM "app"\."events"\), 1\), true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := SyncSequencesToData(context.Background(), sqlDB, []string{"public", "app"}); err != nil {
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

func TestSyncSequencesToDataPropagatesQueryError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnError(errors.New("boom"))

	err = SyncSequencesToData(context.Background(), sqlDB, []string{"public"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSyncSequencesToDataPropagatesSetvalError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT table_schema, table_name, column_name`).
		WithArgs("public").
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "column_name"}).
			AddRow("public", "users", "id"))
	mock.ExpectExec(`SELECT setval`).WillReturnError(errors.New("boom"))

	err = SyncSequencesToData(context.Background(), sqlDB, []string{"public"})
	if err == nil {
		t.Fatal("expected error")
	}
}
