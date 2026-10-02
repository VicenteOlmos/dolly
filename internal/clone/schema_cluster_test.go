package clone

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFormatEnsureRoleAndTablespace(t *testing.T) {
	role := formatEnsureRole(roleSpec{
		name: "app", inherit: true, login: true, connLimit: -1,
		password:   sql.NullString{String: "secret", Valid: true},
		validUntil: sql.NullString{String: "infinity", Valid: true},
		comment:    sql.NullString{String: "app role", Valid: true},
	})
	for _, want := range []string{
		`CREATE ROLE "app";`,
		`ALTER ROLE "app" WITH NOSUPERUSER INHERIT NOCREATEROLE NOCREATEDB LOGIN NOREPLICATION NOBYPASSRLS CONNECTION LIMIT -1 PASSWORD 'secret' VALID UNTIL 'infinity';`,
		`COMMENT ON ROLE "app" IS 'app role';`,
		"EXCEPTION WHEN duplicate_object THEN NULL;",
	} {
		if !strings.Contains(role, want) {
			t.Fatalf("role SQL missing %q\n%s", want, role)
		}
	}
	grant := formatGrantRole(roleGrant{parent: "parent", member: "app", inherit: true, set: true})
	if grant != `GRANT "parent" TO "app" WITH ADMIN FALSE, INHERIT TRUE, SET TRUE` {
		t.Fatalf("grant = %s", grant)
	}
	ts := formatCreateTablespace(tablespaceSpec{
		name: "fast", owner: "app", location: "/data/fast",
		options: map[string]string{"seq_page_cost": "1.2", "effective_io_concurrency": "32"},
	})
	if ts != `CREATE TABLESPACE "fast" OWNER "app" LOCATION '/data/fast' WITH (effective_io_concurrency=32, seq_page_cost=1.2)` {
		t.Fatalf("tablespace = %s", ts)
	}
}

func TestApplyClusterGlobalsCreatesMissingObjects(t *testing.T) {
	src, srcMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })

	srcMock.ExpectQuery(`FROM pg_roles r`).WillReturnRows(sqlmock.NewRows([]string{
		"rolname", "rolsuper", "rolinherit", "rolcreaterole", "rolcreatedb",
		"rolcanlogin", "rolreplication", "rolbypassrls", "rolconnlimit", "rolvaliduntil", "comment",
	}).AddRow("app", false, true, false, false, true, false, false, -1, nil, "app role"))
	srcMock.ExpectQuery(`FROM pg_authid`).WillReturnError(&pgconn.PgError{Code: "42501"})
	srcMock.ExpectQuery(`pg_auth_members`).WillReturnRows(sqlmock.NewRows([]string{
		"parent", "member", "admin", "inherit", "set",
	}).AddRow("pg_read_all_data", "app", false, true, true))
	srcMock.ExpectQuery(`pg_tablespace_location`).WillReturnRows(sqlmock.NewRows([]string{
		"spcname", "owner", "location", "comment",
	}).AddRow("fast", "app", "/data/fast", "fast disks"))
	srcMock.ExpectQuery(`pg_options_to_table\(t.spcoptions\)`).WillReturnRows(sqlmock.NewRows([]string{
		"spcname", "option_name", "option_value",
	}).AddRow("fast", "seq_page_cost", "1.2"))

	tgtMock.ExpectExec(`CREATE ROLE "app"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`GRANT "pg_read_all_data" TO "app"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectQuery(`SELECT EXISTS`).WithArgs("fast").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	tgtMock.ExpectExec(`CREATE TABLESPACE "fast"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`COMMENT ON TABLESPACE "fast"`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := applyClusterGlobals(context.Background(), src, tgt); err != nil {
		t.Fatal(err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureTablespaceSkipsExisting(t *testing.T) {
	tgt, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs("fast").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	if err := ensureTablespace(context.Background(), tgt, tablespaceSpec{
		name: "fast", owner: "app", location: "/data/fast",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClusterPreflightWarnings(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(`spcname NOT IN`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`pg_roles WHERE oid >= 16384`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`FROM pg_foreign_server`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`FROM pg_authid`).WillReturnError(&pgconn.PgError{Code: "42501"})
	mock.ExpectQuery(`FROM pg_user_mapping`).WillReturnError(&pgconn.PgError{Code: "42501"})

	warnings, err := clusterPreflightWarnings(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %v", warnings)
	}
	if !strings.Contains(warnings[0], "2 tablespace(s)") {
		t.Fatalf("tablespace warning = %q", warnings[0])
	}
	if !strings.Contains(warnings[1], "without a password") {
		t.Fatalf("password warning = %q", warnings[1])
	}
	if !strings.Contains(warnings[2], "user mapping options") {
		t.Fatalf("mapping warning = %q", warnings[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
