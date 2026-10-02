package clone

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func expectEmptySchemaCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_extension`).WillReturnRows(
		sqlmock.NewRows([]string{"extname", "nspname", "extversion"}))
	srcMock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "enumlabel"}))
	srcMock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "format_type", "typnotnull", "pg_get_expr"}))
	srcMock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "conname", "pg_get_constraintdef"}))
	srcMock.ExpectQuery(`SHOW server_version_num`).WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(160000))
	srcMock.ExpectQuery(`pg_collation`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "collname", "collprovider", "colliculocale", "collicurules", "collcollate", "collctype", "collisdeterministic",
		}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`pg_range`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "typname", "subtype", "opc_schema", "opcname", "coll_schema", "collname",
			"can_schema", "can_name", "diff_schema", "diff_name", "multirange",
		}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`pg_sequence`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "format_type"}))
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
	expectClassicInheritCatalog(srcMock)
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
	// loadAllPrimaryConstraints (1 query).
	srcMock.ExpectQuery(`constraint_type = 'PRIMARY KEY'[\s\S]*table_schema IN \(\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ordinal_position"}))
	srcMock.ExpectQuery(`con\.contype = 'p'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "conname", "condeferrable", "condeferred"}))
	// loadAllCheckConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'c'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allChecks)
	// loadAllExcludeConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'x'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "conname", "pg_get_constraintdef"}))
	// loadAllForeignKeyConstraints (1 query).
	srcMock.ExpectQuery(`con\.contype = 'f'[\s\S]*nspname IN \(\$1`).
		WillReturnRows(allFKDefs)
	srcMock.ExpectQuery(`a\.attinhcount`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname"}))
}

func expectSecurityLabelsCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_seclabel`).WillReturnRows(
		sqlmock.NewRows([]string{"provider", "kind", "schema", "object", "column", "label"}))
}

func expectPublicationCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_publication p`).WillReturnRows(
		sqlmock.NewRows([]string{"pubname", "pubinsert", "pubupdate", "pubdelete", "pubtruncate", "puballtables", "pubviaroot"}))
	srcMock.ExpectQuery(`pg_publication_namespace`).WillReturnRows(
		sqlmock.NewRows([]string{"pubname", "nspname"}))
	srcMock.ExpectQuery(`pg_publication_rel`).WillReturnRows(
		sqlmock.NewRows([]string{"pubname", "nspname", "relname", "prqual", "colnames"}))
}

func expectForeignTableCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`pg_foreign_table`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "srvname", "option_name", "option_value", "relispartition", "bound", "parent_schema", "parent_name"}))
	srcMock.ExpectQuery(`attfdwoptions`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "sql_type", "nullable", "option_name", "option_value"}))
}

func expectPostTableCatalog(srcMock sqlmock.Sqlmock) {
	expectForeignTableCatalog(srcMock)
	srcMock.ExpectQuery(`relreplident`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}))
	srcMock.ExpectQuery(`attstorage`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstorage"}))
	srcMock.ExpectQuery(`attcompression`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attcompression"}))
	srcMock.ExpectQuery(`pg_options_to_table`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "option_name", "option_value"}))
	srcMock.ExpectQuery(`a\.attstattarget >= 0`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstattarget"}))
	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef", "inherited"}))
	expectPublicationCatalog(srcMock)
	srcMock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(
		sqlmock.NewRows([]string{"pg_get_statisticsobjdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind", "populated", "options"}))
	srcMock.ExpectQuery(`pg_rewrite`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname"}))
	srcMock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	expectSecurityLabelsCatalog(srcMock)
	srcMock.ExpectQuery(`relkind IN \('r', 'p', 'v', 'm', 'f'\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(n\.nspacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(a\.attacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`c\.relkind = 'S'`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "sequence", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(p\.proacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "proname", "pg_get_function_identity_arguments", "kind", "rolname", "privilege_type", "grantable", "missing_public"}))
	srcMock.ExpectQuery(`acldefault\('T', t\.typowner\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "grantee", "grantable"}))
	srcMock.ExpectQuery(`FROM pg_default_acl`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "owner", "defaclobjtype", "grantee", "privilege_type", "grantable", "revoke_public"}))
	srcMock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}))
	srcMock.ExpectQuery(`FROM pg_policy`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "polcmd", "polpermissive", "polqual", "polwithcheck", "roles",
		}))
}

func expectClassicInheritCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_inherits`).WillReturnRows(
		sqlmock.NewRows([]string{"child_schema", "child_name", "parent_schema", "parent_name"}))
}

func expectRoutineCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`a\.aggkind IN \('n', 'o', 'h'\)`).WillReturnRows(sqlmock.NewRows([]string{"def"}))
	srcMock.ExpectQuery(`pg_get_functiondef`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "name", "pg_get_functiondef"}))
	srcMock.ExpectQuery(`JOIN pg_proc ref`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "oid"}))
	srcMock.ExpectQuery(`FROM pg_operator`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "oprname", "nspname", "proname", "left", "right"}))
	srcMock.ExpectQuery(`FROM pg_opfamily`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "opfname", "amname"}))
	srcMock.ExpectQuery(`FROM pg_opclass opc`).WillReturnRows(
		sqlmock.NewRows([]string{
			"oid", "nspname", "opcname", "opcdefault", "input_type", "amname",
			"fam_nspname", "opfname", "storage_type",
		}))
	srcMock.ExpectQuery(`pg_amop amop`).WillReturnRows(
		sqlmock.NewRows([]string{
			"oid", "strategy", "op_nspname", "oprname", "left_type", "right_type",
			"purpose", "sort_nspname", "sort_opfname",
		}))
	srcMock.ExpectQuery(`pg_amproc amproc`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "support", "fn_nspname", "proname", "fn_args"}))
	srcMock.ExpectQuery(`FROM pg_amop amop`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "strategy", "op_nspname", "oprname", "left_type", "right_type",
		"purpose", "sort_nspname", "sort_opfname",
	}))
	srcMock.ExpectQuery(`FROM pg_amproc amproc`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "support", "fn_nspname", "proname", "fn_args",
	}))
	srcMock.ExpectQuery(`FROM pg_cast`).WillReturnRows(
		sqlmock.NewRows([]string{
			"src_schema", "src_name", "tgt_schema", "tgt_name",
			"castmethod", "castcontext", "fn_schema", "fn_name", "fn_args",
		}))
	expectPreTableCatalog(srcMock)
}

func expectPreTableCatalog(srcMock sqlmock.Sqlmock) {
	srcMock.ExpectQuery(`FROM pg_event_trigger`).WillReturnRows(
		sqlmock.NewRows([]string{
			"evtname", "evtevent", "evtenabled", "evttags", "nspname", "proname", "pg_get_function_identity_arguments", "prokind",
		}))
	srcMock.ExpectQuery(`FROM pg_ts_dict`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "dictname", "nspname", "tmplname", "dictinitoption"}))
	srcMock.ExpectQuery(`FROM pg_ts_config c`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "cfgname", "nspname", "prsname"}))
	srcMock.ExpectQuery(`FROM pg_ts_config_map`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "cfgname", "alias", "nspname", "dictname", "mapseqno", "maptokentype",
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

func TestLoadCollationsCatalogVersions(t *testing.T) {
	for _, tt := range []struct {
		major  int
		locale string
		rules  string
	}{
		{14, "coll.collcollate", "''"},
		{16, "coll.colliculocale", "COALESCE(coll.collicurules, '')"},
		{17, "coll.colllocale", "COALESCE(coll.collicurules, '')"},
	} {
		t.Run(strconv.Itoa(tt.major), func(t *testing.T) {
			src, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = src.Close() })
			wantRules := "&V << w"
			if tt.major == 14 {
				wantRules = ""
			}
			mock.ExpectQuery(`SHOW server_version_num`).WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(tt.major * 10000))
			mock.ExpectQuery(regexp.QuoteMeta("COALESCE("+tt.locale+", ''), "+tt.rules) + `(?s).*coll\.collprovider IN \('c', 'i'\)`).
				WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "provider", "locale", "rules", "collate", "ctype", "deterministic"}).
					AddRow("app", "custom", "i", "und", wantRules, "", "", true))
			rows, err := loadCollations(context.Background(), src, []string{"app"})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].icuLocale != "und" || rows[0].icuRules != wantRules {
				t.Fatalf("collations = %+v", rows)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
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
		sqlmock.NewRows([]string{"extname", "nspname", "extversion"}).AddRow("uuid-ossp", "public", ""))
	srcMock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "enumlabel"}).
			AddRow("app", "status_enum", "active").
			AddRow("app", "status_enum", "inactive"))
	srcMock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "format_type", "typnotnull", "pg_get_expr"}))
	srcMock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "conname", "pg_get_constraintdef"}))
	srcMock.ExpectQuery(`SHOW server_version_num`).WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(160000))
	srcMock.ExpectQuery(`pg_collation`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "collname", "collprovider", "colliculocale", "collicurules", "collcollate", "collctype", "collisdeterministic",
		}))
	srcMock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname"}))
	srcMock.ExpectQuery(`pg_range`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "typname", "subtype", "opc_schema", "opcname", "coll_schema", "collname",
			"can_schema", "can_name", "diff_schema", "diff_name", "multirange",
		}))
	srcMock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(
		sqlmock.NewRows([]string{
			"schemaname", "sequencename", "increment_by", "min_value", "max_value", "start_value", "cache_size", "cycle",
		}))
	srcMock.ExpectQuery(`pg_sequence`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "format_type"}))
	srcMock.ExpectQuery(`dep\.deptype IN`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname", "attname", "identity"}))

	expectRoutineCatalog(srcMock)
	srcMock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}))
	expectClassicInheritCatalog(srcMock)

	expectForeignTableCatalog(srcMock)
	srcMock.ExpectQuery(`relreplident`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}))
	srcMock.ExpectQuery(`attstorage`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstorage"}))
	srcMock.ExpectQuery(`attcompression`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attcompression"}))
	srcMock.ExpectQuery(`pg_options_to_table`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "option_name", "option_value"}))
	srcMock.ExpectQuery(`a\.attstattarget >= 0`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "attname", "attstattarget"}))
	srcMock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(
		sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef", "inherited"}))
	expectPublicationCatalog(srcMock)
	srcMock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(
		sqlmock.NewRows([]string{"pg_get_statisticsobjdef"}))
	srcMock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "pg_get_viewdef", "relkind", "populated", "reloptions"}).
			AddRow("app", "active_users", "SELECT id FROM users", false, true, "security_barrier=true,check_option=local"))
	srcMock.ExpectQuery(`pg_rewrite`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "nspname", "relname"}))
	srcMock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "table", "name", "mode", "definition"}))
	srcMock.ExpectQuery(`FROM pg_description`).WillReturnRows(
		sqlmock.NewRows([]string{"kind", "nspname", "relname", "attname", "description"}))
	expectSecurityLabelsCatalog(srcMock)
	srcMock.ExpectQuery(`relkind IN \('r', 'p', 'v', 'm', 'f'\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(n\.nspacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(a\.attacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`c\.relkind = 'S'`).WillReturnRows(
		sqlmock.NewRows([]string{"schema", "sequence", "grantee", "privilege_type", "grantable"}))
	srcMock.ExpectQuery(`aclexplode\(p\.proacl\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "proname", "pg_get_function_identity_arguments", "kind", "rolname", "privilege_type", "grantable", "missing_public"}))
	srcMock.ExpectQuery(`acldefault\('T', t\.typowner\)`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "typname", "grantee", "grantable"}))
	srcMock.ExpectQuery(`FROM pg_default_acl`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "owner", "defaclobjtype", "grantee", "privilege_type", "grantable", "revoke_public"}))
	srcMock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "relname", "relforcerowsecurity"}))
	srcMock.ExpectQuery(`FROM pg_policy`).WillReturnRows(
		sqlmock.NewRows([]string{
			"nspname", "relname", "polname", "polcmd", "polpermissive", "polqual", "polwithcheck", "roles",
		}))

	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE SCHEMA IF NOT EXISTS "app"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA "public"`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE TYPE "app"."status_enum" AS ENUM ('active', 'inactive')`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`CREATE VIEW "app"."active_users" AS SELECT id FROM users`)).WillReturnResult(sqlmock.NewResult(0, 0))
	tgtMock.ExpectExec(regexp.QuoteMeta(`ALTER VIEW "app"."active_users" SET (security_barrier=true, check_option=local)`)).WillReturnResult(sqlmock.NewResult(0, 0))

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
	mock.ExpectQuery(`FROM pg_extension`).WillReturnRows(sqlmock.NewRows([]string{"extname", "nspname", "extversion"}))
	mock.ExpectQuery(`t\.typtype = 'e'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "label"}))
	mock.ExpectQuery(`t\.typtype = 'd'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "base", "notnull", "default"}).AddRow("app", "positive", "integer", false, ""))
	mock.ExpectQuery(`t\.typtype = 'd' AND c\.contype = 'c'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "domain", "name", "def"}).AddRow("app", "positive", "valid", "CHECK (app.valid_value(VALUE))"))
	mock.ExpectQuery(`SHOW server_version_num`).WillReturnRows(sqlmock.NewRows([]string{"server_version_num"}).AddRow(160000))
	mock.ExpectQuery(`pg_collation`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "collname", "collprovider", "colliculocale", "collicurules", "collcollate", "collctype", "collisdeterministic",
	}))
	mock.ExpectQuery(`t\.typtype = 'c'`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name"}))
	mock.ExpectQuery(`pg_range`).WillReturnRows(sqlmock.NewRows([]string{
		"schema", "name", "subtype", "opc_schema", "opc", "coll_schema", "coll",
		"can_schema", "can_name", "diff_schema", "diff_name", "mr_schema", "multirange",
	}).AddRow("app", "span", "integer", "pg_catalog", "int4_ops", "", "", "", "", "", "", "", "").
		AddRow("app", "span2", "integer", "", "", "", "", "app", "span2_canonical", "app", "span2_diff", "app", "span2_set"))
	mock.ExpectQuery(`FROM pg_sequences`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "increment", "min", "max", "start", "cache", "cycle"}))
	mock.ExpectQuery(`pg_sequence`).WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "format_type"}))
	mock.ExpectQuery(`dep\.deptype IN`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "table_schema", "table_name", "column", "identity"}))
	mock.ExpectQuery(`a\.aggkind IN \('n', 'o', 'h'\)`).WillReturnRows(sqlmock.NewRows([]string{"def"}))
	mock.ExpectQuery(`pg_get_functiondef`).WillReturnRows(sqlmock.NewRows([]string{"oid", "name", "def"}).AddRow(1, "app.valid_value(integer)", "CREATE FUNCTION app.valid_value(integer) RETURNS boolean LANGUAGE sql AS 'SELECT true'"))
	mock.ExpectQuery(`JOIN pg_proc ref`).WillReturnRows(sqlmock.NewRows([]string{"oid", "ref"}))
	mock.ExpectQuery(`FROM pg_operator`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "oprname", "nspname", "proname", "left", "right"}))
	mock.ExpectQuery(`FROM pg_opfamily`).WillReturnRows(
		sqlmock.NewRows([]string{"nspname", "opfname", "amname"}))
	mock.ExpectQuery(`FROM pg_opclass opc`).WillReturnRows(
		sqlmock.NewRows([]string{
			"oid", "nspname", "opcname", "opcdefault", "input_type", "amname",
			"fam_nspname", "opfname", "storage_type",
		}))
	mock.ExpectQuery(`pg_amop amop`).WillReturnRows(
		sqlmock.NewRows([]string{
			"oid", "strategy", "op_nspname", "oprname", "left_type", "right_type",
			"purpose", "sort_nspname", "sort_opfname",
		}))
	mock.ExpectQuery(`pg_amproc amproc`).WillReturnRows(
		sqlmock.NewRows([]string{"oid", "support", "fn_nspname", "proname", "fn_args"}))
	mock.ExpectQuery(`FROM pg_amop amop`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "strategy", "op_nspname", "oprname", "left_type", "right_type",
		"purpose", "sort_nspname", "sort_opfname",
	}))
	mock.ExpectQuery(`FROM pg_amproc amproc`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "support", "fn_nspname", "proname", "fn_args",
	}))
	mock.ExpectQuery(`FROM pg_cast`).WillReturnRows(sqlmock.NewRows([]string{
		"src_schema", "src_name", "tgt_schema", "tgt_name",
		"castmethod", "castcontext", "fn_schema", "fn_name", "fn_args",
	}))
	expectPreTableCatalog(mock)
	mock.ExpectQuery(`SELECT t\.table_schema`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "count"}))
	expectClassicInheritCatalog(mock)
	expectForeignTableCatalog(mock)
	mock.ExpectQuery(`relreplident`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "ident", "index"}).AddRow("app", "items", "i", "items_code_idx"))
	mock.ExpectQuery(`attstorage`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column", "storage"}).AddRow("app", "items", "code", "e"))
	mock.ExpectQuery(`attcompression`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column", "compression"}))
	mock.ExpectQuery(`pg_options_to_table`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "option_name", "option_value"}))
	mock.ExpectQuery(`a\.attstattarget >= 0`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "column", "target"}))
	mock.ExpectQuery(`FROM pg_indexes`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "def", "inherited"}).AddRow("app", "items", "items_code_idx", `CREATE UNIQUE INDEX "items_code_idx" ON "app"."items" (code)`, false))
	expectPublicationCatalog(mock)
	mock.ExpectQuery(`pg_get_statisticsobjdef`).WillReturnRows(sqlmock.NewRows([]string{"def"}).AddRow(`CREATE STATISTICS app.mv_stats ON id, value FROM app.mv`))
	mock.ExpectQuery(`pg_get_viewdef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "name", "def", "materialized", "populated", "options"}).AddRow("app", "mv", "SELECT 1 AS id, 2 AS value", true, true, "fillfactor=70"))
	mock.ExpectQuery(`pg_rewrite`).WillReturnRows(sqlmock.NewRows([]string{"schema", "view", "ref_schema", "ref_view"}))
	mock.ExpectQuery(`pg_get_triggerdef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}))
	mock.ExpectQuery(`pg_get_ruledef`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}))
	mock.ExpectQuery(`FROM pg_description`).WillReturnRows(sqlmock.NewRows([]string{"kind", "schema", "object", "column", "description"}).
		AddRow("domain_constraint", "app", "positive", "valid", "must be positive").
		AddRow("policy", "app", "items", "tenant", "tenant filter").
		AddRow("trigger", "app", "items", "touch", "keeps updated_at").
		AddRow("rule", "app", "items", "log_del", "audit"))
	expectSecurityLabelsCatalog(mock)
	mock.ExpectQuery(`c\.relrowsecurity`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "force"}).AddRow("app", "items", false))
	mock.ExpectQuery(`FROM pg_policy`).WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "command", "permissive", "using", "check", "roles"}).
		AddRow("app", "items", "tenant", "SELECT", true, "true", "", ""))

	rec := &scriptExec{}
	if err := applySchemas(context.Background(), src, rec, []string{"app"}, false); err != nil {
		t.Fatal(err)
	}
	script := rec.String()
	parts := []string{"CREATE DOMAIN", `CREATE TYPE "app"."span" AS RANGE`, "CREATE FUNCTION", "ALTER DOMAIN", "ALTER TABLE ONLY", "CREATE UNIQUE INDEX", "REPLICA IDENTITY USING INDEX", "CREATE MATERIALIZED VIEW", "ALTER MATERIALIZED VIEW", "CREATE STATISTICS", "COMMENT ON CONSTRAINT", "COMMENT ON TRIGGER", "COMMENT ON RULE", "CREATE POLICY", "COMMENT ON POLICY"}
	last := -1
	for _, part := range parts {
		pos := strings.Index(script, part)
		if pos <= last {
			t.Fatalf("%s not ordered after prior statement:\n%s", part, script)
		}
		last = pos
	}
	shell := strings.Index(script, `CREATE TYPE "app"."span2";`)
	full := strings.Index(script, `CREATE TYPE "app"."span2" AS RANGE`)
	fn := strings.Index(script, "CREATE FUNCTION")
	if shell < 0 || full < 0 || shell >= fn || fn >= full {
		t.Fatalf("range shell order shell=%d fn=%d full=%d\n%s", shell, fn, full, script)
	}
	if !strings.Contains(script, `CANONICAL = "app"."span2_canonical"`) || strings.Contains(script, "span2_canonical(") {
		t.Fatalf("canonical must be a bare name:\n%s", script)
	}
	if !strings.Contains(script, `SUBTYPE_DIFF = "app"."span2_diff"`) || !strings.Contains(script, `MULTIRANGE_TYPE_NAME = "app"."span2_set"`) {
		t.Fatalf("missing range options:\n%s", script)
	}
	if !strings.Contains(script, `ALTER MATERIALIZED VIEW "app"."mv" SET (fillfactor=70)`) {
		t.Fatalf("missing matview fillfactor:\n%s", script)
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
		got, err := formatCreateTable(table, cols, nil, []uniqueConstraint{tt.unique}, nil, nil, nil)
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
	got, err := formatCreateTable(parent, cols, nil, nil, nil, nil, nil)
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
	got, err = formatCreateTable(child, cols, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want = `CREATE TABLE "public"."events_2024" PARTITION OF "public"."events" FOR VALUES FROM (1) TO (2)`
	if got != want {
		t.Fatalf("child SQL =\n%s\nwant\n%s", got, want)
	}

	pk := primaryConstraint{name: "events_2024_pkey", columns: []string{"id"}}
	got, err = formatCreateTable(child, cols, &pk, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want = `CREATE TABLE "public"."events_2024" PARTITION OF "public"."events" (CONSTRAINT "events_2024_pkey" PRIMARY KEY ("id")) FOR VALUES FROM (1) TO (2)`
	if got != want {
		t.Fatalf("partition local primary key SQL =\n%s\nwant\n%s", got, want)
	}

	child.RelKind = "p"
	child.PartitionBy = "LIST (total)"
	cols[0].defaultExpr = sql.NullString{String: "42", Valid: true}
	got, err = formatCreateTable(child, cols, nil, nil, []checkConstraint{{name: "positive", def: "CHECK (id > 0)"}}, nil, nil)
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
	got, err := formatCreateTable(unloggedParent, cols, nil, nil, nil, nil, nil)
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
	got, err = formatCreateTable(child, cols[:1], nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "UNLOGGED") {
		t.Fatalf("partition child must not repeat UNLOGGED: %s", got)
	}
}

func TestFormatCreateTableClassicInherits(t *testing.T) {
	parent := db.Table{Schema: "app", Name: "base"}
	child := db.Table{Schema: "app", Name: "derived"}
	cols := []schemaColumn{
		{name: "id", sqlType: "integer", nullable: false},
		{name: "extra", sqlType: "text", nullable: true},
	}
	got, err := formatCreateTable(child, []schemaColumn{cols[1]}, nil, nil, nil, nil, []inheritParent{{schema: "app", name: "base"}})
	if err != nil {
		t.Fatal(err)
	}
	want := `CREATE TABLE "app"."derived" ("extra" text) INHERITS ("app"."base")`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	gotParent, err := formatCreateTable(parent, cols[:1], nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotParent, "INHERITS") {
		t.Fatalf("parent should not inherit: %s", gotParent)
	}
}

func TestOrderPartitionParentsFirst(t *testing.T) {
	tables := []db.Table{
		{Schema: "public", Name: "events_2024", PartitionOf: "public.events"},
		{Schema: "public", Name: "events", RelKind: "p", PartitionBy: "RANGE (id)"},
	}
	got := orderPartitionParentsFirst(tables, nil)
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
	stmt, err := formatCreateTable(db.Table{Schema: "app", Name: "items"}, []schemaColumn{{name: "id", sqlType: "bigint", identityGen: "ALWAYS", identitySeq: &seq}}, nil, nil, nil, nil, nil)
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

func TestFormatCreateTableDeferrablePrimary(t *testing.T) {
	t.Parallel()
	table := db.Table{
		Schema:  "app",
		Name:    "items",
		Columns: []db.Column{{Name: "id", PrimaryKey: true}},
	}
	cols := []schemaColumn{{name: "id", sqlType: "integer", nullable: false}}
	tests := []struct {
		name string
		pk   primaryConstraint
		want string
	}{
		{
			name: "non_deferrable",
			pk:   primaryConstraint{name: "items_pkey", columns: []string{"id"}},
			want: `CONSTRAINT "items_pkey" PRIMARY KEY ("id")`,
		},
		{
			name: "deferrable_immediate",
			pk:   primaryConstraint{name: "items_pkey", columns: []string{"id"}, deferrable: true},
			want: `CONSTRAINT "items_pkey" PRIMARY KEY ("id") DEFERRABLE`,
		},
		{
			name: "deferrable_deferred",
			pk:   primaryConstraint{name: "items_pkey", columns: []string{"id"}, deferrable: true, deferred: true},
			want: `CONSTRAINT "items_pkey" PRIMARY KEY ("id") DEFERRABLE INITIALLY DEFERRED`,
		},
	}
	for _, tt := range tests {
		got, err := formatCreateTable(table, cols, &tt.pk, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if !strings.Contains(got, tt.want) {
			t.Fatalf("%s: got %q, want substring %q", tt.name, got, tt.want)
		}
		if strings.Contains(got, `PRIMARY KEY ("id")`) && strings.Count(got, "PRIMARY KEY") > 1 {
			t.Fatalf("%s: duplicate inline primary key: %q", tt.name, got)
		}
	}
}

func TestLoadIndexesQueryOmitsExclusionBackingIndex(t *testing.T) {
	src, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	mock.ExpectQuery(`contype IN \('p', 'u', 'x'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"schemaname", "tablename", "indexname", "indexdef", "inherited"}))

	indexes, err := loadIndexes(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(indexes) != 0 {
		t.Fatalf("indexes = %+v", indexes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatCreateTableExcludeConstraint(t *testing.T) {
	t.Parallel()
	table := db.Table{Schema: "app", Name: "bookings"}
	cols := []schemaColumn{
		{name: "room", sqlType: "integer", nullable: false},
		{name: "during", sqlType: "tsrange", nullable: false},
	}
	exc := excludeConstraint{
		name: "bookings_room_during_excl",
		def:  "EXCLUDE USING gist (room WITH =, during WITH &&)",
	}
	got, err := formatCreateTable(table, cols, nil, nil, nil, []excludeConstraint{exc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `CONSTRAINT "bookings_room_during_excl" EXCLUDE USING gist (room WITH =, during WITH &&)`
	if !strings.Contains(got, want) {
		t.Fatalf("got %q, want substring %q", got, want)
	}
}

func TestLoadReplicaIdentitiesIncludesPartitions(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`(?s)relreplident.*n\.nspname IN \(\$1\)`).
		WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}).
			AddRow("app", "events_2024", "f", ""))
	rows, err := loadReplicaIdentities(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].table != "events_2024" {
		t.Fatalf("rows = %+v", rows)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReplicaIdentitiesQueryOmitsPartitionFilter(t *testing.T) {
	src, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		if strings.Contains(actual, "relispartition") {
			return fmt.Errorf("replica identity query must include partition children")
		}
		return nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`.*`).WillReturnRows(sqlmock.NewRows([]string{"nspname", "relname", "relreplident", "indexname"}))
	if _, err := loadReplicaIdentities(context.Background(), src, []string{"app"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTriggersSkipsExtensionOwned(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`pg_get_triggerdef`).
		WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}).
			AddRow("app", "items", "user_touch", "O", `CREATE TRIGGER "user_touch" BEFORE UPDATE ON "app"."items" FOR EACH ROW EXECUTE FUNCTION touch()`))
	defs, err := loadTriggers(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || !strings.Contains(defs[0], "user_touch") {
		t.Fatalf("defs = %v", defs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRulesSkipsExtensionOwned(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	mock.ExpectQuery(`pg_get_ruledef`).
		WillReturnRows(sqlmock.NewRows([]string{"schema", "table", "name", "mode", "def"}).
			AddRow("app", "items", "log_del", "O", `CREATE RULE "log_del" AS ON DELETE TO "app"."items" DO INSTEAD NOTHING`))
	defs, err := loadRules(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || !strings.Contains(defs[0], "log_del") {
		t.Fatalf("defs = %v", defs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFormatAggregateHypothetical(t *testing.T) {
	got := formatAggregate(aggregateSpec{
		schema: "public", name: "my_rank", args: "integer, integer",
		aggkind: "h", aggNumDirect: 1, stype: "integer", sfuncSchema: "public", sfunc: "hypo_step",
		parallel: "s", initVal: sql.NullString{String: "0", Valid: true},
	})
	want := `CREATE AGGREGATE "public"."my_rank"(integer ORDER BY integer) (SFUNC = "public"."hypo_step", STYPE = integer, INITCOND = '0', PARALLEL = SAFE, HYPOTHETICAL)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestFormatAggregateOrderedSet(t *testing.T) {
	got := formatAggregate(aggregateSpec{
		schema: "public", name: "percentile_cont", args: "double precision, double precision",
		aggkind: "o", aggNumDirect: 1, stype: "float8", sfuncSchema: "pg_catalog", sfunc: "float8_accum",
		parallel: "u",
	})
	want := `CREATE AGGREGATE "public"."percentile_cont"(double precision ORDER BY double precision) (SFUNC = "pg_catalog"."float8_accum", STYPE = float8, PARALLEL = UNSAFE)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestFormatCreatePublication(t *testing.T) {
	got := formatCreatePublication(publicationSpec{
		name: "events_pub", insert: true, update: false, delete: true, truncate: true,
		tables: []publicationTable{
			{schema: "app", name: "users"},
			{schema: "app", name: "events"},
		},
	})
	want := `CREATE PUBLICATION "events_pub" FOR ONLY "app"."users", ONLY "app"."events" WITH (publish = 'insert, delete, truncate')`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestFormatCreatePublicationFilteredTable(t *testing.T) {
	got := formatCreatePublication(publicationSpec{
		name: "filtered_pub", insert: true, update: true, delete: true, truncate: true,
		tables: []publicationTable{
			{schema: "app", name: "events", columns: []string{"id", "name"}, qual: "id > 0"},
		},
	})
	want := `CREATE PUBLICATION "filtered_pub" FOR ONLY "app"."events" ("id", "name") WHERE (id > 0)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestFormatCreatePublicationSchemaAndAllTables(t *testing.T) {
	got := formatCreatePublication(publicationSpec{
		name: "mixed_pub", insert: true, update: true, delete: true, truncate: true,
		allTables:               true,
		publishViaPartitionRoot: true,
		schemas:                 []string{"billing"},
		tables: []publicationTable{
			{schema: "app", name: "users"},
		},
	})
	want := `CREATE PUBLICATION "mixed_pub" FOR ALL TABLES, TABLES IN SCHEMA "billing", ONLY "app"."users" WITH (publish_via_partition_root = true)`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestFormatCreateOperator(t *testing.T) {
	got := formatCreateOperator(operatorSpec{
		schema: "app", name: "===", funcSchema: "app", funcName: "eq_text",
		leftType: "text", rightType: "text",
	})
	if !strings.Contains(got, `CREATE OPERATOR "app"."==="`) || !strings.Contains(got, "LEFTARG = text") {
		t.Fatalf("got %s", got)
	}
}
