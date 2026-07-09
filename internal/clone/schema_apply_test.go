package clone

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func expectEmptySchemaCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_extension`).WillReturnRows(
		sqlmock.NewRows([]string{"extname"}))
	srcMock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "enumlabel"}))
	srcMock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "format_type", "typnotnull", "pg_get_expr"}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`dep\.deptype = 'a'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname", "attname"}))
}

func expectLoadTableIntrospection(srcMock sqlmock.Sqlmock, colMeta, fks *sqlmock.Rows) {
	srcMock.ExpectQuery(`FROM information_schema.columns c`).WillReturnRows(colMeta)
	srcMock.ExpectQuery(`constraint_type = 'FOREIGN KEY'`).WillReturnRows(fks)
}

func expectApplyTableDDL(srcMock sqlmock.Sqlmock, cols, unique, checks *sqlmock.Rows) {
	srcMock.ExpectQuery(`FROM information_schema.columns`).WillReturnRows(cols)
	srcMock.ExpectQuery(`constraint_type = 'UNIQUE'`).WillReturnRows(unique)
	srcMock.ExpectQuery(`con\.contype = 'c'`).WillReturnRows(checks)
}

// expectBatchedSchemaObjects sets up the batched variants of all per-table
// introspection queries used by ApplySchemasFromSource after P4 batching.
func expectBatchedSchemaObjects(srcMock sqlmock.Sqlmock, schemaCount string, allCols, allFks, allDDLCols, allUniques, allChecks, allFKDefs *sqlmock.Rows) {
	// fetchAllColumns (from LoadPostgresSchemas in db package, 1 query).
	srcMock.ExpectQuery(`FROM information_schema.columns c[\s\S]*ORDER BY c\.table_schema, c\.table_name, c\.ordinal_position`).
		WillReturnRows(allCols)
	// fetchAllForeignKeys (1 query).
	srcMock.ExpectQuery(`tc\.constraint_type = 'FOREIGN KEY'[\s\S]*table_schema IN \(\$1[\s\S]*ORDER BY tc\.table_schema, tc\.table_name, tc\.constraint_name, kcu\.column_name`).
		WillReturnRows(allFks)
	// loadAllSchemaColumns (1 query).
	srcMock.ExpectQuery(`FROM information_schema.columns[\s\S]*table_schema IN \(\$1[\s\S]*ORDER BY table_schema, table_name, ordinal_position`).
		WillReturnRows(allDDLCols)
	// loadAllUniqueConstraints (1 query).
	srcMock.ExpectQuery(`constraint_type = 'UNIQUE'[\s\S]*table_schema IN \(\$1`).
		WillReturnRows(allUniques)
	// loadAllCheckConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'c'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allChecks)
	// loadAllForeignKeyConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'f'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allFKDefs)
}

func expectPostTableCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind"}))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	srcMock.ExpectQuery(`FROM information_schema.table_privileges`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "grantee", "privilege_type"}))
	srcMock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}))
	srcMock.ExpectQuery(`FROM pg_policy`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "polcmd", "polpermissive", "polqual", "polwithcheck", "roles",
		}))
}

func TestColumnSQLType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dataType  string
		charMax   sql.NullInt64
		numPrec   sql.NullInt64
		numScale  sql.NullInt64
		udtName   sql.NullString
		udtSchema sql.NullString
		want      string
	}{
		{
			dataType: "character varying",
			charMax:  sql.NullInt64{Int64: 50, Valid: true},
			want:     "character varying(50)",
		},
		{
			dataType: "numeric",
			numPrec:  sql.NullInt64{Int64: 10, Valid: true},
			numScale: sql.NullInt64{Int64: 2, Valid: true},
			want:     "numeric(10,2)",
		},
		{
			dataType:  "USER-DEFINED",
			udtName:   sql.NullString{String: "status_enum", Valid: true},
			udtSchema: sql.NullString{String: "billing", Valid: true},
			want:      `"billing"."status_enum"`,
		},
		{
			dataType: "integer",
			want:     "integer",
		},
	}
	for _, tt := range tests {
		got := columnSQLType(tt.dataType, tt.charMax, tt.numPrec, tt.numScale, tt.udtName, tt.udtSchema)
		if got != tt.want {
			t.Fatalf("columnSQLType() = %q, want %q", got, tt.want)
		}
	}
}

func TestApplySchemasFromSourceCreatesSchemaAndTable(t *testing.T) {
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

	expectEmptySchemaCatalog(srcMock)

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "users", int64(0))
	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	// Batched: fetchAllColumns from LoadPostgresSchemas.
	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "users", "id", "integer", "NO", 1, true).
		AddRow("app", "users", "name", "character varying", "YES", 2, false)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})

	// Batched: loadAllSchemaColumns.
	allDDLCols := sqlmock.NewRows([]string{
		"table_schema", "table_name", "column_name", "data_type", "is_nullable", "column_default", "ordinal_position",
		"character_maximum_length", "numeric_precision", "numeric_scale", "udt_name", "udt_schema",
	}).AddRow("app", "users", "id", "integer", "NO", nil, 1, nil, nil, nil, nil, nil).
		AddRow("app", "users", "name", "character varying", "YES", nil, 2, int64(50), nil, nil, nil, nil)
	allUniques := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ordinal_position"})
	allChecks := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"})
	allFKDefs := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"})

	expectBatchedSchemaObjects(srcMock, "$1", allCols, allFks, allDDLCols, allUniques, allChecks, allFKDefs)

	expectPostTableCatalog(srcMock)

	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "app"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE TABLE "app"\."users".*PRIMARY KEY \("id"\)`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := ApplySchemasFromSource(context.Background(), src, tgt, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplySchemasFromSourceRichDDL(t *testing.T) {
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

	const schema = "billing"

	expectEmptySchemaCatalog(srcMock)

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow(schema, "accounts", int64(0))
	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	// Batched: fetchAllColumns from LoadPostgresSchemas.
	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow(schema, "accounts", "id", "bigint", "NO", 1, true).
		AddRow(schema, "accounts", "email", "character varying", "NO", 2, false).
		AddRow(schema, "accounts", "status", "USER-DEFINED", "YES", 3, false)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})

	allDDLCols := sqlmock.NewRows([]string{
		"table_schema", "table_name", "column_name", "data_type", "is_nullable", "column_default", "ordinal_position",
		"character_maximum_length", "numeric_precision", "numeric_scale", "udt_name", "udt_schema",
	}).
		AddRow(schema, "accounts", "id", "bigint", "NO", nil, 1, nil, nil, nil, nil, nil).
		AddRow(schema, "accounts", "email", "character varying", "NO", nil, 2, int64(255), nil, nil, nil, nil).
		AddRow(schema, "accounts", "status", "USER-DEFINED", "YES", `'active'::billing.status_enum`, 3, nil, nil, nil, "status_enum", schema)
	allUniques := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ordinal_position"}).
		AddRow(schema, "accounts", "accounts_email_key", "email", 1)
	allChecks := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"}).
		AddRow(schema, "accounts", "accounts_amount_positive", "CHECK (amount > 0)")
	allFKDefs := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"})

	expectBatchedSchemaObjects(srcMock, "$1", allCols, allFks, allDDLCols, allUniques, allChecks, allFKDefs)

	expectPostTableCatalog(srcMock)

	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "billing"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE TABLE "billing"\."accounts".*PRIMARY KEY \("id"\).*CONSTRAINT "accounts_email_key" UNIQUE \("email"\).*CHECK \(amount > 0\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := ApplySchemasFromSource(context.Background(), src, tgt, []string{schema}); err != nil {
		t.Fatal(err)
	}

	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplySchemasFromSourceForeignKeyQualified(t *testing.T) {
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

	expectEmptySchemaCatalog(srcMock)

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "users", int64(0)).
		AddRow("billing", "accounts", int64(0))
	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(tablesRows)

	// Batched: fetchAllColumns (LoadPostgresSchemas) with 2 tables.
	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "users", "id", "integer", "NO", 1, true).
		AddRow("billing", "accounts", "id", "integer", "NO", 1, true).
		AddRow("billing", "accounts", "user_id", "integer", "NO", 2, false)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}).
		AddRow("billing", "accounts", "accounts_user_id_fkey", "user_id", "app", "users", "id")

	allDDLCols := sqlmock.NewRows([]string{
		"table_schema", "table_name", "column_name", "data_type", "is_nullable", "column_default", "ordinal_position",
		"character_maximum_length", "numeric_precision", "numeric_scale", "udt_name", "udt_schema",
	}).
		AddRow("app", "users", "id", "integer", "NO", nil, 1, nil, nil, nil, nil, nil).
		AddRow("billing", "accounts", "id", "integer", "NO", nil, 1, nil, nil, nil, nil, nil).
		AddRow("billing", "accounts", "user_id", "integer", "NO", nil, 2, nil, nil, nil, nil, nil)
	allUniques := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ordinal_position"})
	allChecks := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"})

	fkDef := `FOREIGN KEY ("user_id") REFERENCES "app"."users" ("id") ON DELETE CASCADE`
	allFKDefs := sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"}).
		AddRow("billing", "accounts", "accounts_user_id_fkey", fkDef)

	// Multi-schema batched: 2 schemas → $1, $2
	expectBatchedSchemaObjects(srcMock, "$2", allCols, allFks, allDDLCols, allUniques, allChecks, allFKDefs)

	expectPostTableCatalog(srcMock)

	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "app"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "billing"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE TABLE "app"\."users"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`CREATE TABLE "billing"\."accounts"`).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(`ALTER TABLE "billing"\."accounts" ADD CONSTRAINT "accounts_user_id_fkey" FOREIGN KEY \("user_id"\) REFERENCES "app"\."users" \("id"\) ON DELETE CASCADE`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := ApplySchemasFromSource(context.Background(), src, tgt, []string{"app", "billing"}); err != nil {
		t.Fatal(err)
	}

	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplySchemasFromSourceEnumExtensionView(t *testing.T) {
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

	srcMock.ExpectQuery(`FROM pg_extension`).WillReturnRows(
		sqlmock.NewRows([]string{"extname"}).AddRow("uuid-ossp"))
	srcMock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "enumlabel"}).
			AddRow("app", "status_enum", "active").
			AddRow("app", "status_enum", "inactive"))
	srcMock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "format_type", "typnotnull", "pg_get_expr"}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`dep\.deptype = 'a'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname", "attname"}))

	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}))

	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind"}).
			AddRow("app", "active_users", "SELECT id FROM users", false))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	srcMock.ExpectQuery(`FROM information_schema.table_privileges`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "grantee", "privilege_type"}))
	srcMock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}))
	srcMock.ExpectQuery(`FROM pg_policy`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "polcmd", "polpermissive", "polqual", "polwithcheck", "roles",
		}))

	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "app"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE TYPE "app"."status_enum" AS ENUM ('active', 'inactive')`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE VIEW "app"."active_users" AS SELECT id FROM users`)).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := ApplySchemasFromSource(context.Background(), src, tgt, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	if err := srcMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
