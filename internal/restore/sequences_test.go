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

// expectSequenceCurrentValueLess returns a current value lower than the dump
// value so the monotonic check allows setval to proceed.
func expectSequenceCurrentValueLess(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(1, true))
}

func expectSequenceDefinition(mock sqlmock.Sqlmock, dataType string, increment, min, max, cache int64, cycle bool) {
	mock.ExpectQuery(`FROM pg_catalog\.pg_sequences`).
		WillReturnRows(sqlmock.NewRows([]string{"data_type", "increment_by", "min_value", "max_value", "cache_size", "cycle"}).
			AddRow(dataType, increment, min, max, cache, cycle))
}

func TestRestoreSequencesFromMetadataRestoresOwnedSequence(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	expectSequenceOwner(mock, "public", "users", "id")
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, false\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataAcceptsOmittedPartitionParent(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := dump.Metadata{
		Tables: []db.Table{{Schema: "public", Name: "events_p0", Columns: []db.Column{{Name: "id"}}}},
		Sequences: []dump.SequenceState{{
			Schema: "public", Name: "events_id_seq", StartValue: 5,
		}},
		Provenance: &dump.Provenance{OmittedPartitionParents: []string{"public.events"}},
	}
	expectSequenceOwner(mock, "public", "events", "id")
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval\('"public"\."events_id_seq"'::regclass, 5, false\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesFromMetadataRejectsUnrelatedOwnerWhenParentsOmitted(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := dump.Metadata{
		Tables: []db.Table{{Schema: "public", Name: "events_p0", Columns: []db.Column{{Name: "id"}}}},
		Sequences: []dump.SequenceState{{
			Schema: "public", Name: "other_seq", StartValue: 5,
		}},
		Provenance: &dump.Provenance{OmittedPartitionParents: []string{"public.events"}},
	}
	expectSequenceOwner(mock, "private", "secrets", "id")
	err = RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not owned by a restored column") {
		t.Fatalf("err = %v", err)
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
	err = RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil)
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
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
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
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 5, false\)`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
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
	expectSequenceCurrentValueLess(mock)
	mock.ExpectExec(`SELECT setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, []string{"public"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreSequencesFromMetadataMonotonicSkipsHigherTarget verifies that when
// the target current value is higher than the dump value, setval is skipped and
// the sequence is not lowered. This is the core monotonic invariant.
func TestRestoreSequencesFromMetadataMonotonicSkipsHigherTarget(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(5)
	meta.Sequences[0].IsCalled = true
	expectSequenceOwner(mock, "public", "users", "id")
	// Current value higher than dump: should skip setval
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(100, true))
	// No setval expected — the monotonic check should skip it.
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreSequencesFromMetadataMonotonicAppliesLowerTarget verifies that when
// the target current value is lower than the dump value, setval is still applied.
func TestRestoreSequencesFromMetadataMonotonicAppliesLowerTarget(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(50)
	meta.Sequences[0].IsCalled = true
	expectSequenceOwner(mock, "public", "users", "id")
	// Current value lower than dump: should apply setval
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(10, true))
	mock.ExpectExec(`SELECT setval\('"public"\."users_id_seq"'::regclass, 50, true\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreSequencesFromMetadataMonotonicSkipsHigherTargetNotCalled verifies
// that when target is_called is false but last_value is higher than the dump,
// setval is skipped regardless of is_called. Contract: any valid target
// last_value >= dump value MUST skip setval.
func TestRestoreSequencesFromMetadataMonotonicSkipsHigherTargetNotCalled(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(5)
	meta.Sequences[0].IsCalled = true
	expectSequenceOwner(mock, "public", "users", "id")
	// Target is_called=false, last_value=100 > dump=5: must skip setval.
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(100, false))
	// No setval Exec expected — the monotonic guard must skip it.
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreSequencesFromMetadataMonotonicReadErrorFailsClosed verifies that
// a failure to read the current sequence value is fatal.
func TestRestoreSequencesFromMetadataMonotonicReadErrorFailsClosed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(5)
	meta.Sequences[0].IsCalled = true
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnError(context.DeadlineExceeded)
	err = RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "read current value") {
		t.Fatalf("error missing context: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func ptrBool(v bool) *bool { return &v }

func TestFormatAlterSequenceOptions(t *testing.T) {
	t.Parallel()
	inc, min, max, cache := int64(5), int64(1), int64(1000), int64(3)
	got, ok, err := formatAlterSequenceOptions(dump.SequenceState{
		Schema: "app", Name: "items_id_seq",
		IncrementBy: &inc, MinValue: &min, MaxValue: &max, CacheSize: &cache,
		Cycle: ptrBool(true), DataType: "integer",
	})
	if err != nil || !ok {
		t.Fatalf("expected options, err=%v", err)
	}
	want := `ALTER SEQUENCE "app"."items_id_seq" AS integer INCREMENT BY 5 MINVALUE 1 MAXVALUE 1000 CACHE 3 CYCLE`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got, ok, err = formatAlterSequenceOptions(dump.SequenceState{
		Schema: "app", Name: "items_id_seq", DataType: "bigint",
		IncrementBy: &inc, Cycle: ptrBool(false),
	})
	if err != nil || !ok || !strings.Contains(got, "AS bigint") {
		t.Fatalf("bigint must emit AS bigint: %q err=%v", got, err)
	}
	if _, ok, err := formatAlterSequenceOptions(dump.SequenceState{Schema: "app", Name: "old"}); err != nil || ok {
		t.Fatal("legacy metadata must not alter")
	}
	for _, bad := range []string{"integer; SELECT pg_sleep(10); --", "bigint; DROP TABLE t; --", "text"} {
		if _, _, err := formatAlterSequenceOptions(dump.SequenceState{DataType: bad}); err == nil {
			t.Fatalf("data_type %q must be rejected", bad)
		}
	}
}

func TestRestoreSequencesAppliesOptionsBeforeSetval(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	inc := int64(2)
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].IncrementBy = &inc
	meta.Sequences[0].DataType = "integer"
	meta.Sequences[0].Cycle = ptrBool(false)
	expectSequenceOwner(mock, "public", "users", "id")
	expectSequenceCurrentValueLess(mock)
	expectSequenceDefinition(mock, "bigint", 1, 1, 100, 1, true)
	mock.ExpectExec(`ALTER SEQUENCE "public"\."users_id_seq" AS integer INCREMENT BY 2 NO CYCLE`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`SELECT setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesAppliesOptionsWhenSetvalSkipped(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	inc := int64(4)
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(5)
	meta.Sequences[0].IncrementBy = &inc
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(100, true))
	expectSequenceDefinition(mock, "bigint", 1, 1, 1000, 1, false)
	mock.ExpectExec(`^ALTER SEQUENCE "public"\."users_id_seq" INCREMENT BY 4$`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesSkipsAlterWhenDefinitionMatches(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	inc := int64(2)
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].IncrementBy = &inc
	meta.Sequences[0].DataType = "integer"
	meta.Sequences[0].Cycle = ptrBool(false)
	expectSequenceOwner(mock, "public", "users", "id")
	expectSequenceCurrentValueLess(mock)
	expectSequenceDefinition(mock, "integer", 2, 1, 1000, 1, false)
	mock.ExpectExec(`SELECT setval`).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesRejectsUnsafeDataType(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	for _, bad := range []string{"integer; SELECT pg_sleep(10); --", "bigint; DROP TABLE t; --", "text"} {
		meta := sequenceMetadata("public", "users", "id", "users_id_seq")
		meta.Sequences[0].DataType = bad
		err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "data_type") {
			t.Fatalf("data_type %q: err = %v", bad, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSequencesOmitsBoundsThatExcludeAdvancedValue(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	inc := int64(4)
	max := int64(100)
	meta := sequenceMetadata("public", "users", "id", "users_id_seq")
	meta.Sequences[0].LastValue = ptrInt64(5)
	meta.Sequences[0].IncrementBy = &inc
	meta.Sequences[0].MaxValue = &max
	meta.Sequences[0].Cycle = ptrBool(true)
	meta.Sequences[0].DataType = "bigint"
	expectSequenceOwner(mock, "public", "users", "id")
	mock.ExpectQuery(`SELECT last_value, is_called`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(1000, true))
	expectSequenceDefinition(mock, "integer", 1, 1, 1000000, 1, false)
	mock.ExpectExec(`^ALTER SEQUENCE "public"\."users_id_seq" AS bigint INCREMENT BY 4$`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	if err := RestoreSequencesFromMetadata(context.Background(), sqlDB, meta, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
