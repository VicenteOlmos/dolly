package clone

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadRLSTablesQuerySkipsViews(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`relkind IN \('r', 'p'\)\s+AND n\.nspname`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}).
			AddRow("app", "items", true),
	)
	tables, err := loadRLSTables(context.Background(), db, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0].table != "items" {
		t.Fatalf("tables = %+v", tables)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPoliciesQueryPreservesPublicRole(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`role_oid = 0 THEN 'PUBLIC'`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "command", "polpermissive", "polqual", "polwithcheck", "roles",
		}).AddRow("app", "items", "mixed", "ALL", true, "true", "", "PUBLIC,app_reader"),
	)
	policies, err := loadPolicies(context.Background(), db, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 1 {
		t.Fatalf("policies = %+v", policies)
	}
	got := formatCreatePolicy(policies[0].schema, policies[0].table, policies[0].pol)
	want := `CREATE POLICY "mixed" ON "app"."items" AS PERMISSIVE FOR ALL TO PUBLIC, "app_reader" USING (true)`
	if got != want {
		t.Fatalf("policy = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
