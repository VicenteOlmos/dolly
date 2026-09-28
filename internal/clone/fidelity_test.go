package clone

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSetUserTriggersAndRefreshMaterializedViews(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT n.nspname, c.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("app", "users"),
	)
	mock.ExpectExec(`ALTER TABLE "app"."users" DISABLE TRIGGER USER`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := setUserTriggers(context.Background(), db, false); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`SELECT n.nspname, c.relname`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname"}).AddRow("app", "users"),
	)
	mock.ExpectExec(`ALTER TABLE "app"."users" ENABLE TRIGGER USER`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := setUserTriggers(context.Background(), db, true); err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`SELECT schemaname, matviewname`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "matviewname"}).AddRow("app", "user_stats"),
	)
	mock.ExpectExec(`REFRESH MATERIALIZED VIEW "app"."user_stats"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := refreshMaterializedViews(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
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
