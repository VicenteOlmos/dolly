package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFormatRowCountMismatch(t *testing.T) {
	got := formatRowCountMismatch("public", "orders", 10, 8)
	want := "verify: public.orders row count source=10 target=8"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatSequenceLastValueMismatch(t *testing.T) {
	got := formatSequenceLastValueMismatch("public", "orders_id_seq", 5, 1)
	want := "verify: public.orders_id_seq last_value source=5 target=1"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVerifyGapWarnings(t *testing.T) {
	w := verifyGapWarnings(SchemaReplayGapCounts{HypotheticalAggregates: 2})
	if len(w) != 1 {
		t.Fatalf("warnings = %d, want 1", len(w))
	}
	if w[0] != "verify: schema-replay did not copy 2 hypothetical aggregate(s); pg_dump is required for those objects" {
		t.Fatalf("unexpected hypothetical warning: %q", w[0])
	}
}

func TestVerifySequencesReadsRelationState(t *testing.T) {
	src, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	srcMock.ExpectQuery(`FROM pg_class c`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("public", "orders_id_seq"))
	srcMock.ExpectQuery(`SELECT last_value, is_called FROM "public"\."orders_id_seq"`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(int64(5), true))
	tgtMock.ExpectQuery(`FROM pg_class c`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("public", "orders_id_seq"))
	tgtMock.ExpectQuery(`SELECT last_value, is_called FROM "public"\."orders_id_seq"`).
		WillReturnRows(sqlmock.NewRows([]string{"last_value", "is_called"}).AddRow(int64(1), false))

	warnings, err := verifySequences(context.Background(), src, tgt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %#v, want last_value and is_called", warnings)
	}
	if warnings[0] != "verify: public.orders_id_seq last_value source=5 target=1" {
		t.Fatalf("last_value warning = %q", warnings[0])
	}
	if warnings[1] != "verify: public.orders_id_seq is_called source=true target=false" {
		t.Fatalf("is_called warning = %q", warnings[1])
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifySequencesUsageOnlyDoesNotFail(t *testing.T) {
	src, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer tgt.Close()

	denied := &pgconn.PgError{Code: "42501", Message: "permission denied for sequence orders_id_seq"}
	for _, mock := range []sqlmock.Sqlmock{srcMock, tgtMock} {
		mock.ExpectQuery(`FROM pg_class c`).
			WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("app", "orders_id_seq"))
		mock.ExpectQuery(`SELECT last_value, is_called FROM`).
			WillReturnError(denied)
		mock.ExpectQuery(`FROM pg_sequences`).
			WillReturnRows(sqlmock.NewRows([]string{"last_value"}).AddRow(nil))
	}

	warnings, err := verifySequences(context.Background(), src, tgt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "verify: app.orders_id_seq is_called unavailable without sequence SELECT") {
		t.Fatalf("warnings = %#v", warnings)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
