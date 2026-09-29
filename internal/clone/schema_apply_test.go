package clone

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func expectEmptySchemaCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_extension`).WillReturnRows(
		sqlmock.NewRows([]string{"extname"}))
	srcMock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "enumlabel"}))
	srcMock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "format_type", "typnotnull", "pg_get_expr"}))
	srcMock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "conname", "pg_get_constraintdef"}))
	srcMock.ExpectQuery(`pg_collation`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "collname", "collprovider", "colliculocale", "collcollate", "collctype", "collisdeterministic",
		}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`dep\.deptype IN`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname", "attname", "identity"}))
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
	// fetchUniqueIndexes (from LoadPostgresSchemas in db package, 1 query).
	srcMock.ExpectQuery(`pg_index[\s\S]*indisunique`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "relname", "index_name", "index_oid", "indisprimary",
		"indisvalid", "indisready", "amname", "has_predicate",
		"is_expression", "indnkeyatts", "attname", "is_nullable", "pos",
		"attnum", "opclass_oid", "collation_oid", "optval",
	}))
	// loadAllSchemaColumns (1 query).
	srcMock.ExpectQuery(`FROM information_schema.columns[\s\S]*table_schema IN \(\$1[\s\S]*ORDER BY table_schema, table_name, ordinal_position`).
		WillReturnRows(allDDLCols)
	srcMock.ExpectQuery(`is_generated = 'ALWAYS'`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "generation_expression"}))
	srcMock.ExpectQuery(`format_type`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "format_type", "identity", "coll_schema", "coll_name"}))
	// loadAllUniqueConstraints (1 query).
	srcMock.ExpectQuery(`constraint_type = 'UNIQUE'[\s\S]*table_schema IN \(\$1`).
		WillReturnRows(allUniques)
	srcMock.ExpectQuery(`con\.contype = 'u'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "conname", "condeferrable", "condeferred"}))
	// loadAllCheckConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'c'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allChecks)
	// loadAllForeignKeyConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'f'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allFKDefs)
}

func expectPostTableCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`relreplident`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}))
	srcMock.ExpectQuery(`attstorage`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstorage"}))
	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef", "inherited"}))
	srcMock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(
		sqlmock.NewRows([]string{"pg_get_statisticsobjdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind"}))
	srcMock.ExpectQuery(`pg_rewrite`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname"}))
	srcMock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	srcMock.ExpectQuery(`FROM information_schema.table_privileges`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "grantee", "privilege_type"}))
	srcMock.ExpectQuery(`aclexplode\(a\.attacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(c\.relacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "sequence", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(p\.proacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "proname", "pg_get_function_identity_arguments", "kind", "rolname", "privilege_type", "grantable", "missing_public"}))
	srcMock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}))
	srcMock.ExpectQuery(`FROM pg_policy`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "polcmd", "polpermissive", "polqual", "polwithcheck", "roles",
		}))
}

func expectRoutineCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`a\.aggkind <> 'n'`).WillReturnRows(sqlmock.NewRows([]string{"name", "aggkind"}))
	srcMock.ExpectQuery(`a\.aggkind = 'n'`).WillReturnRows(sqlmock.NewRows([]string{"def"}))
	srcMock.ExpectQuery(`pg_get_functiondef`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "name", "pg_get_functiondef"}))
	srcMock.ExpectQuery(`JOIN pg_proc ref`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "oid"}))
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
	expectRoutineCatalog(srcMock)

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
	expectRoutineCatalog(srcMock)

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
	expectRoutineCatalog(srcMock)

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
	srcMock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "conname", "pg_get_constraintdef"}))
	srcMock.ExpectQuery(`pg_collation`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "collname", "collprovider", "colliculocale", "collcollate", "collctype", "collisdeterministic",
		}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`dep\.deptype IN`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname", "attname", "identity"}))

	expectRoutineCatalog(srcMock)
	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}))

	srcMock.ExpectQuery(`relreplident`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}))
	srcMock.ExpectQuery(`attstorage`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstorage"}))
	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef", "inherited"}))
	srcMock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(
		sqlmock.NewRows([]string{"pg_get_statisticsobjdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind"}).
			AddRow("app", "active_users", "SELECT id FROM users", false))
	srcMock.ExpectQuery(`pg_rewrite`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname"}))
	srcMock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	srcMock.ExpectQuery(`FROM information_schema.table_privileges`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "grantee", "privilege_type"}))
	srcMock.ExpectQuery(`aclexplode\(a\.attacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(c\.relacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "sequence", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(p\.proacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "proname", "pg_get_function_identity_arguments", "kind", "rolname", "privilege_type", "grantable", "missing_public"}))
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

func TestApplySchemasOrdersDomainChecksAndViewStatistics(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`FROM pg_extension`).WillReturnRows(sqlmock.NewRows([]string{"extname"}))
	mock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "label"}))
	mock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "base", "notnull", "default"}).AddRow("app", "positive", "integer", false, ""))
	mock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "domain", "name", "def"}).AddRow("app", "positive", "valid", "CHECK (app.valid_value(VALUE))"))
	mock.ExpectQuery(`pg_collation`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "collname", "collprovider", "colliculocale", "collcollate", "collctype", "collisdeterministic",
	}))
	mock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name"}))
	mock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "increment", "min", "max", "start", "cache", "cycle"}))
	mock.ExpectQuery(`dep\.deptype IN`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "table_schema", "table_name", "column", "identity"}))
	mock.ExpectQuery(`a\.aggkind <> 'n'`).WillReturnRows(sqlmock.NewRows([]string{"name", "kind"}))
	mock.ExpectQuery(`a\.aggkind = 'n'`).WillReturnRows(sqlmock.NewRows([]string{"def"}))
	mock.ExpectQuery(`pg_get_functiondef`).WillReturnRows(sqlmock.NewRows([]string{"oid", "name", "def"}).AddRow(1, "app.valid_value(integer)", "CREATE FUNCTION app.valid_value(integer) RETURNS boolean LANGUAGE sql AS 'SELECT true'"))
	mock.ExpectQuery(`JOIN pg_proc ref`).WillReturnRows(sqlmock.NewRows([]string{"oid", "ref"}))
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "count"}))
	mock.ExpectQuery(`relreplident`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "ident", "index"}).AddRow("app", "items", "i", "items_code_idx"))
	mock.ExpectQuery(`attstorage`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column", "storage"}).AddRow("app", "items", "code", "e"))
	mock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "def", "inherited"}).AddRow("app", "items", "items_code_idx", `CREATE UNIQUE INDEX "items_code_idx" ON "app"."items" (code)`, false))
	mock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(sqlmock.NewRows([]string{"def"}).AddRow(`CREATE STATISTICS app.mv_stats ON id, value FROM app.mv`))
	mock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "def", "materialized"}).AddRow("app", "mv", "SELECT 1 AS id, 2 AS value", true))
	mock.ExpectQuery(`pg_rewrite`).WillReturnRows(sqlmock.NewRows([]string{"schema", "view", "ref_schema", "ref_view"}))
	mock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}))
	mock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}))
	mock.ExpectQuery(`FROM pg_description`).WillReturnRows(sqlmock.NewRows([]string{"kind", "schema", "object", "column", "description"}).AddRow("domain_constraint", "app", "positive", "valid", "must be positive"))
	mock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "force"}))
	mock.ExpectQuery(`FROM pg_policy`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "command", "permissive", "using", "check", "roles"}))

	rec := &scriptExec{}
	if err := applySchemas(context.Background(), src, rec, []string{"app"}, false); err != nil {
		t.Fatal(err)
	}
	script := rec.String()
	parts := []string{"CREATE DOMAIN", "CREATE FUNCTION", "ALTER DOMAIN", "ALTER TABLE ONLY", "CREATE UNIQUE INDEX", "REPLICA IDENTITY USING INDEX", "CREATE MATERIALIZED VIEW", "CREATE STATISTICS", "COMMENT ON CONSTRAINT"}
	last := -1
	for _, part := range parts {
		pos := strings.Index(script, part)
		if pos <= last {
			t.Fatalf("%s not ordered after prior statement:\n%s", part, script)
		}
		last = pos
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatCreateTableDeferrableUnique(t *testing.T) {
	t.Parallel()
	table := db.Table{Schema: "app", Name: "items"}
	cols := []schemaColumn{{name: "code", sqlType: "text", nullable: false}}
	tests := []struct {
		name   string
		unique uniqueConstraint
		want   string
	}{
		{
			name:   "non_deferrable",
			unique: uniqueConstraint{name: "items_code_key", columns: []string{"code"}},
			want:   `CONSTRAINT "items_code_key" UNIQUE ("code")`,
		},
		{
			name:   "deferrable_immediate",
			unique: uniqueConstraint{name: "items_code_key", columns: []string{"code"}, deferrable: true},
			want:   `CONSTRAINT "items_code_key" UNIQUE ("code") DEFERRABLE`,
		},
		{
			name:   "deferrable_deferred",
			unique: uniqueConstraint{name: "items_code_key", columns: []string{"code"}, deferrable: true, deferred: true},
			want:   `CONSTRAINT "items_code_key" UNIQUE ("code") DEFERRABLE INITIALLY DEFERRED`,
		},
	}
	for _, tt := range tests {
		got, err := formatCreateTable(table, cols, []uniqueConstraint{tt.unique}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !strings.Contains(got, tt.want) {
			t.Fatalf("%s: got %q, want substring %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatCreateTablePartitionAndGenerated(t *testing.T) {
	parent := db.Table{
		Schema:      "public",
		Name:        "events",
		RelKind:     "p",
		PartitionBy: "RANGE (id)",
		Columns:     []db.Column{{Name: "id", PrimaryKey: true}},
	}
	cols := []schemaColumn{
		{name: "id", sqlType: "integer"},
		{name: "total", sqlType: "integer", generatedExpr: "id * 2"},
	}
	got, err := formatCreateTable(parent, cols, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE TABLE "public"."events" ("id" integer NOT NULL, "total" integer GENERATED ALWAYS AS (id * 2) STORED NOT NULL, PRIMARY KEY ("id")) PARTITION BY RANGE (id)`
	if got != want {
		t.Fatalf("parent SQL =\n%s\nwant\n%s", got, want)
	}

	child := db.Table{
		Schema:         "public",
		Name:           "events_2024",
		PartitionOf:    "public.events",
		PartitionBound: "FOR VALUES FROM (1) TO (2)",
	}
	got, err = formatCreateTable(child, cols, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want = `CREATE TABLE "public"."events_2024" PARTITION OF "public"."events" FOR VALUES FROM (1) TO (2)`
	if got != want {
		t.Fatalf("child SQL =\n%s\nwant\n%s", got, want)
	}
	child.RelKind = "p"
	child.PartitionBy = "LIST (total)"
	cols[0].defaultExpr = sql.NullString{String: "42", Valid: true}
	got, err = formatCreateTable(child, cols, nil, []checkConstraint{{name: "positive", def: "CHECK (id > 0)"}})
	if err != nil {
		t.Fatal(err)
	}
	want = `CREATE TABLE "public"."events_2024" PARTITION OF "public"."events" ("id" WITH OPTIONS DEFAULT 42, CONSTRAINT "positive" CHECK (id > 0)) FOR VALUES FROM (1) TO (2) PARTITION BY LIST (total)`
	if got != want {
		t.Fatalf("nested child SQL =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatCreateTableIdentityCollateUnlogged(t *testing.T) {
	cols := []schemaColumn{
		{name: "tags", sqlType: "integer[]", nullable: false},
		{name: "id", sqlType: "bigint", identityGen: "BY DEFAULT", nullable: false},
		{name: "label", sqlType: "text", collationSchema: "public", collationName: "und", nullable: true},
	}
	unloggedParent := db.Table{Schema: "public", Name: "cache", Unlogged: true}
	got, err := formatCreateTable(unloggedParent, cols, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"tags" integer[] NOT NULL`) {
		t.Fatalf("array type: %s", got)
	}
	if !strings.Contains(got, `GENERATED BY DEFAULT AS IDENTITY`) {
		t.Fatalf("identity: %s", got)
	}
	if !strings.Contains(got, `COLLATE "public"."und"`) {
		t.Fatalf("collation: %s", got)
	}
	if !strings.HasPrefix(got, `CREATE UNLOGGED TABLE "public"."cache"`) {
		t.Fatalf("unlogged parent: %s", got)
	}

	child := db.Table{
		Schema:         "public",
		Name:           "cache_p1",
		PartitionOf:    "public.cache",
		PartitionBound: "FOR VALUES IN ('a')",
		Unlogged:       true,
	}
	got, err = formatCreateTable(child, cols[:1], nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "UNLOGGED") {
		t.Fatalf("partition child must not repeat UNLOGGED: %s", got)
	}
}

func TestOrderPartitionParentsFirst(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events_2024", PartitionOf: "public.events"},
		{Schema: "public", Name: "events", RelKind: "p", PartitionBy: "RANGE (id)"},
	}
	got := orderPartitionParentsFirst(tables)
	if got[0].Name != "events" || got[1].Name != "events_2024" {
		t.Fatalf("order = %s, %s", got[0].Name, got[1].Name)
	}
}

func TestIndexesForReplaySkipsPartitionChildren(t *testing.T) {
	indexes := []indexRow{
		{schema: "public", table: "events", name: "events_id_idx", def: "CREATE INDEX events_id_idx", inherited: true},
		{schema: "public", table: "events_2024", name: "events_2024_id_idx", def: "CREATE INDEX events_2024_id_idx"},
	}
	tables := []db.Table{{Schema: "public", Name: "events_2024", PartitionOf: "public.events"}}
	got := indexesForReplay(indexes, tables)
	if len(got) != 1 || got[0].table != "events_2024" {
		t.Fatalf("indexes = %+v", got)
	}
	local := []indexRow{
		{schema: "public", table: "events_2024", name: "events_2024_local_idx", def: "CREATE INDEX events_2024_local_idx"},
		{schema: "public", table: "events_2024", name: "events_2024_inh_idx", def: "CREATE INDEX events_2024_inh_idx", inherited: true},
	}
	got = indexesForReplay(local, tables)
	if len(got) != 1 || got[0].name != "events_2024_local_idx" {
		t.Fatalf("local indexes = %+v", got)
	}
}

func TestIndexesForReplayParentBeforeLocalLeaf(t *testing.T) {
	indexes := []indexRow{
		{schema: "app", table: "a_leaf", name: "a_local", def: "CREATE INDEX a_local"},
		{schema: "app", table: "z_parent", name: "z_parent_idx", def: "CREATE INDEX z_parent_idx ON ONLY app.z_parent (m)"},
		{schema: "app", table: "a_leaf", name: "a_inherited", inherited: true},
	}
	tables := []db.Table{
		{Schema: "app", Name: "a_leaf", PartitionOf: "app.z_parent"},
		{Schema: "app", Name: "z_parent", RelKind: "p"},
	}
	got := indexesForReplay(indexes, tables)
	if len(got) != 2 || got[0].name != "z_parent_idx" || got[1].name != "a_local" {
		t.Fatalf("index replay order = %+v", got)
	}
	if strings.Contains(got[0].def, " ON ONLY ") {
		t.Fatalf("partition parent index not propagated: %s", got[0].def)
	}
}

func TestIdentitySequenceDDL(t *testing.T) {
	seq := sequenceRow{
		schema: "app", name: "custom_id_seq", identity: true,
		ownedSchema: "app", ownedTable: "items", ownedColumn: "id",
		def: sequenceDef{increment: 5, minValue: 1, maxValue: 1000, startValue: 11, cache: 3},
	}
	dbMock, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbMock.Close()
	if err := applySequences(context.Background(), dbMock, []sequenceRow{seq}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	stmt, err := formatCreateTable(db.Table{Schema: "app", Name: "items"}, []schemaColumn{{name: "id", sqlType: "bigint", identityGen: "ALWAYS", identitySeq: &seq}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `GENERATED ALWAYS AS IDENTITY (SEQUENCE NAME "app"."custom_id_seq" INCREMENT BY 5 MINVALUE 1 MAXVALUE 1000 START WITH 11 CACHE 3)`
	if !strings.Contains(stmt, want) {
		t.Fatalf("identity definition = %s, want %s", stmt, want)
	}
}

func TestMergeColumnCatalogQualifiedTypes(t *testing.T) {
	dbMock, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dbMock.Close()
	mock.ExpectQuery(`CASE WHEN type_ns\.nspname <> 'pg_catalog'`).WithArgs("app").WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "column", "type", "identity", "coll_schema", "coll_name"}).
			AddRow("app", "items", "mood", `"app"."mood"`, "", "", "").
			AddRow("app", "items", "moods", `"app"."mood"[]`, "", "", ""))
	cols := map[string][]schemaColumn{"app.items": {{name: "mood"}, {name: "moods"}}}
	if err := mergeColumnCatalog(context.Background(), dbMock, []string{"app"}, cols); err != nil {
		t.Fatal(err)
	}
	if cols["app.items"][0].sqlType != `"app"."mood"` || cols["app.items"][1].sqlType != `"app"."mood"[]` {
		t.Fatalf("qualified types = %+v", cols["app.items"])
	}
}

func TestFormatAggregateNormal(t *testing.T) {
	got := formatAggregate(aggregateSpec{
		schema: "public", name: "sum_int", args: "integer",
		stype: "bigint", sfuncSchema: "public", sfunc: "int4_sum",
		parallel: "s", initVal: sql.NullString{String: "0", Valid: true},
	})
	want := `CREATE AGGREGATE "public"."sum_int"(integer) (SFUNC = "public"."int4_sum", STYPE = bigint, INITCOND = '0', PARALLEL = SAFE)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestLoadAggregatesRejectsOrderedSet(t *testing.T) {
	conn, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	mock.ExpectQuery(`a\.aggkind <> 'n'`).WillReturnRows(
		sqlmock.NewRows([]string{"name", "aggkind"}).AddRow(`"public"."percentile"(double precision)`, "o"))
	_, err = loadAggregates(context.Background(), conn, []string{"public"})
	if err == nil || !strings.Contains(err.Error(), "ordered-set aggregate") {
		t.Fatalf("err = %v", err)
	}
}
