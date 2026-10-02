package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestFormatForeignServerStatements(t *testing.T) {
	server := formatCreateForeignServer(foreignServerDef{
		name: "app_srv", fdw: "postgres_fdw", srvType: "pg", version: "16",
		options: map[string]string{"host": "127.0.0.1", "dbname": "postgres"},
	})
	if !strings.Contains(server, `CREATE SERVER "app_srv" TYPE 'pg' VERSION '16' FOREIGN DATA WRAPPER "postgres_fdw"`) {
		t.Fatalf("server = %s", server)
	}
	if !strings.Contains(server, "OPTIONS (dbname 'postgres', host '127.0.0.1')") {
		t.Fatalf("options = %s", server)
	}
	wrapper := formatCreateForeignDataWrapper(foreignDataWrapperDef{
		name: "my_fdw", hasHandler: true, handler: `"public"."my_handler"`,
	})
	if wrapper != `CREATE FOREIGN DATA WRAPPER "my_fdw" HANDLER "public"."my_handler" NO VALIDATOR` {
		t.Fatalf("wrapper = %s", wrapper)
	}
	mapping := formatCreateUserMapping(userMappingDef{
		server: "app_srv", options: map[string]string{"user": "dolly"},
	})
	if mapping != `CREATE USER MAPPING FOR PUBLIC SERVER "app_srv" OPTIONS (user 'dolly')` {
		t.Fatalf("mapping = %s", mapping)
	}
	wrapped := wrapDuplicateObject(mapping)
	if !strings.Contains(wrapped, "EXCEPTION WHEN duplicate_object THEN NULL;") {
		t.Fatalf("wrapped = %s", wrapped)
	}
	newline := wrapDuplicateObject(formatCreateUserMapping(userMappingDef{
		server: "app_srv", options: map[string]string{"password": "first\nsecond"},
	}))
	if strings.Contains(newline, "first;") || !strings.Contains(newline, "first\nsecond") {
		t.Fatalf("mapping newline was split:\n%s", newline)
	}
}

func TestApplyForeignServersSkipsExtensionWrapper(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })

	mock.ExpectQuery(`FROM pg_foreign_server srv`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "fdwname", "srvtype", "srvversion", "rolname", "owned"}).
			AddRow("app_srv", "postgres_fdw", "", "", "dolly", true))
	mock.ExpectQuery(`pg_options_to_table\(srv.srvoptions\)`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "option_name", "option_value"}).
			AddRow("app_srv", "host", "127.0.0.1"))
	mock.ExpectQuery(`FROM pg_user_mapping um`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "rolname", "option_name", "option_value"}).
			AddRow("app_srv", "", "user", "dolly"))

	tgtMock.ExpectExec(`CREATE SERVER "app_srv"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE USER MAPPING FOR PUBLIC`).WillReturnResult(sqlmock.NewResult(0, 0))

	err = applyForeignServers(context.Background(), src, tgt, []foreignTableDef{{
		schema: "app", name: "remote", server: "app_srv",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyForeignServersCreatesLooseWrapper(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })

	mock.ExpectQuery(`FROM pg_foreign_server srv`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "fdwname", "srvtype", "srvversion", "rolname", "owned"}).
			AddRow("custom_srv", "my_fdw", "", "", "dolly", false))
	mock.ExpectQuery(`pg_options_to_table\(srv.srvoptions\)`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "option_name", "option_value"}))
	mock.ExpectQuery(`FROM pg_foreign_data_wrapper fdw`).WillReturnRows(
		sqlmock.NewRows([]string{"fdwname", "has_handler", "hn", "h", "has_valid", "vn", "v"}).
			AddRow("my_fdw", true, "public", "my_handler", false, "", ""))
	mock.ExpectQuery(`pg_options_to_table\(fdw.fdwoptions\)`).WillReturnRows(
		sqlmock.NewRows([]string{"fdwname", "option_name", "option_value"}))
	mock.ExpectQuery(`FROM pg_user_mapping um`).WillReturnError(&pgconn.PgError{Code: "42501"})
	mock.ExpectQuery(`FROM pg_user_mappings m`).WillReturnRows(
		sqlmock.NewRows([]string{"srvname", "usename"}).AddRow("custom_srv", "dolly"))

	tgtMock.ExpectExec(`CREATE FOREIGN DATA WRAPPER "my_fdw"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE SERVER "custom_srv"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE USER MAPPING FOR "dolly"`).WillReturnResult(sqlmock.NewResult(0, 0))

	err = applyForeignServers(context.Background(), src, tgt, []foreignTableDef{{
		schema: "app", name: "remote", server: "custom_srv",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
