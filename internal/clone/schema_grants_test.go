package clone

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestReplayExtraGrants(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	ctx := context.Background()
	schemas := []string{"app"}

	mock.ExpectQuery(`aclexplode\(a\.attacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "column", "grantee", "privilege", "grantable"}).
			AddRow("app", "users", "email", "PUBLIC", "SELECT", false).
			AddRow("app", "users", "email", "reader", "SELECT", false).
			AddRow("app", "users", "email", "reader", "UPDATE", false).
			AddRow("app", "users", "email", "editor", "UPDATE", true))
	columns, err := loadColumnGrants(ctx, src, schemas)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`aclexplode\(c\.relacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "sequence", "grantee", "privilege", "grantable"}).
			AddRow("app", "seq", "PUBLIC", "USAGE", false).
			AddRow("app", "seq", "reader", "SELECT", false).
			AddRow("app", "seq", "editor", "UPDATE", true))
	sequences, err := loadSequenceGrants(ctx, src, schemas)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`aclexplode\(p\.proacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "name", "args", "kind", "grantee", "privilege", "grantable", "missing_public"}).
			AddRow("app", "f", "integer", "FUNCTION", "reader", "EXECUTE", true, true).
			AddRow("app", "f", "integer", "FUNCTION", "owner", "EXECUTE", false, true).
			AddRow("app", "p", "", "PROCEDURE", "PUBLIC", "EXECUTE", false, false).
			AddRow("app", "q", "", "PROCEDURE", "PUBLIC", "", false, true))
	routines, err := loadRoutineGrants(ctx, src, schemas)
	if err != nil {
		t.Fatal(err)
	}

	rec := &scriptExec{}
	for _, apply := range []func() error{
		func() error { return applyColumnGrants(ctx, rec, columns) },
		func() error { return applySequenceGrants(ctx, rec, sequences) },
		func() error { return applyRoutineGrants(ctx, rec, routines) },
	} {
		if err := apply(); err != nil {
			t.Fatal(err)
		}
	}
	script := rec.String()
	for _, stmt := range []string{
		`GRANT SELECT ("email") ON TABLE "app"."users" TO PUBLIC;`,
		`GRANT SELECT ("email"), UPDATE ("email") ON TABLE "app"."users" TO "reader";`,
		`GRANT UPDATE ("email") ON TABLE "app"."users" TO "editor" WITH GRANT OPTION;`,
		`GRANT USAGE ON SEQUENCE "app"."seq" TO PUBLIC;`,
		`GRANT SELECT ON SEQUENCE "app"."seq" TO "reader";`,
		`GRANT UPDATE ON SEQUENCE "app"."seq" TO "editor" WITH GRANT OPTION;`,
		`REVOKE EXECUTE ON FUNCTION "app"."f"(integer) FROM PUBLIC;`,
		`GRANT EXECUTE ON FUNCTION "app"."f"(integer) TO "reader" WITH GRANT OPTION;`,
		`GRANT EXECUTE ON PROCEDURE "app"."p"() TO PUBLIC;`,
		`REVOKE EXECUTE ON PROCEDURE "app"."q"() FROM PUBLIC;`,
	} {
		if !strings.Contains(script, stmt) {
			t.Errorf("missing %s in:\n%s", stmt, script)
		}
	}
	if strings.Count(script, `REVOKE EXECUTE ON FUNCTION "app"."f"`) != 1 {
		t.Errorf("duplicate revoke in:\n%s", script)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProcedureComment(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`CASE WHEN p\.prokind = 'p' THEN 'procedure'`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "schema", "object", "column", "description"}).
			AddRow("procedure", "app", "run(integer)", "", "job"))
	comments, err := loadComments(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &scriptExec{}
	if err := applyComments(context.Background(), rec, comments); err != nil {
		t.Fatal(err)
	}
	if got := rec.String(); got != "COMMENT ON PROCEDURE \"app\".\"run\"(integer) IS 'job';\n" {
		t.Fatalf("comment SQL = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConstraintCommentsOnlyForReplayedConstraints(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`con\.contype = 'u' AND con\.conparentid = 0[\s\S]*con\.contype = 'c' AND con\.coninhcount = 0[\s\S]*con\.contype = 'f' AND con\.conparentid = 0[\s\S]*SELECT 'domain_constraint'[\s\S]*t\.typtype = 'd' AND con\.contype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "schema", "object", "column", "description"}).
			AddRow("domain_constraint", "app", "email", "email_check", "valid email"))
	comments, err := loadComments(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &scriptExec{}
	if err := applyComments(context.Background(), rec, comments); err != nil {
		t.Fatal(err)
	}
	if got := rec.String(); got != `COMMENT ON CONSTRAINT "email_check" ON DOMAIN "app"."email" IS 'valid email';`+"\n" {
		t.Fatalf("comment SQL = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
