package clone

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestApplyTargetFidelityRestoresModesAfterFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT n.nspname, c.relname, t.tgname, t.tgenabled`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "trigger", "mode"}).
			AddRow("app", "users", "audit", "R").AddRow("app", "users", "other", "A").AddRow("app", "users", "off", "D"),
	)
	mock.ExpectExec(`ALTER TABLE "app"."users" DISABLE TRIGGER "audit"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ALTER TABLE "app"."users" DISABLE TRIGGER "other"`).WillReturnError(errors.New("blocked"))
	mock.ExpectExec(`ALTER TABLE "app"."users" ENABLE REPLICA TRIGGER "audit"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := applyTargetFidelityDefault(context.Background(), db, db, func() error {
		t.Fatal("restore must not run after disable failure")
		return nil
	}); err == nil {
		t.Fatal("expected disable error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyTargetFidelityCleanupAfterCancellation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT n.nspname, c.relname, t.tgname, t.tgenabled`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "trigger", "mode"}).AddRow("app", "users", "audit", "A"),
	)
	mock.ExpectExec(`DISABLE TRIGGER "audit"`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ENABLE ALWAYS TRIGGER "audit"`).WillReturnResult(sqlmock.NewResult(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	if err := applyTargetFidelityDefault(ctx, db, db, func() error {
		cancel()
		return context.Canceled
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("restore error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshMaterializedViewsDependenciesAndPopulation(t *testing.T) {
	source, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	srcMock.ExpectQuery(`SELECT n.nspname, c.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name"}).AddRow("app", "a_summary").AddRow("app", "z_base"),
	)
	tgtMock.ExpectQuery(`SELECT schemaname, matviewname FROM pg_matviews`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name"}).AddRow("app", "a_summary").AddRow("app", "z_base"),
	)
	tgtMock.ExpectQuery(`SELECT dn.nspname, dependent.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"ds", "dn", "rs", "rn"}).AddRow("app", "a_summary", "app", "z_base"),
	)
	tgtMock.ExpectExec(`REFRESH MATERIALIZED VIEW "app"."z_base"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`REFRESH MATERIALIZED VIEW "app"."a_summary"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := refreshMaterializedViews(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshMaterializedViewsSkipsUnpopulatedSource(t *testing.T) {
	source, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	srcMock.ExpectQuery(`SELECT n.nspname, c.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name"}).AddRow("app", "populated"),
	)
	tgtMock.ExpectQuery(`SELECT schemaname, matviewname FROM pg_matviews`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name"}).AddRow("app", "populated").AddRow("app", "unpopulated"),
	)
	tgtMock.ExpectQuery(`SELECT dn.nspname, dependent.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"ds", "dn", "rs", "rn"}),
	)
	tgtMock.ExpectExec(`REFRESH MATERIALIZED VIEW "app"."populated"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := refreshMaterializedViews(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaOnlyDumpArgsPrivileges(t *testing.T) {
	plain := schemaOnlyDumpArgs("postgres://u@h/db", []string{"app"}, false)
	want := []string{"--schema-only", "--no-owner", "--no-acl", "--schema=app", "postgres://u@h/db"}
	if !sliceEqual(plain, want) {
		t.Fatalf("plain = %v", plain)
	}
	priv := schemaOnlyDumpArgs("postgres://u@h/db", nil, true)
	wantPriv := []string{"--schema-only", "postgres://u@h/db"}
	if !sliceEqual(priv, wantPriv) {
		t.Fatalf("priv = %v", priv)
	}
}
