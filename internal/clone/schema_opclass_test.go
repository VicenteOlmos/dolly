package clone

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFormatOperatorClassObjects(t *testing.T) {
	t.Parallel()
	fam := formatCreateOperatorFamily(operatorFamilyDef{
		schema: "app",
		name:   "custom_ops",
		method: "btree",
	})
	wantFam := `CREATE OPERATOR FAMILY "app"."custom_ops" USING "btree"`
	if fam != wantFam {
		t.Fatalf("family: got %q, want %q", fam, wantFam)
	}

	cls, ok := formatCreateOperatorClass(operatorClassDef{
		schema:       "app",
		name:         "custom_ops",
		defaultClass: true,
		inputType:    "integer",
		method:       "btree",
		familySchema: "app",
		familyName:   "custom_ops",
		storageType:  "text",
		operators: []operatorClassOperator{{
			strategy:      1,
			opSchema:      "pg_catalog",
			opName:        "<",
			leftType:      "integer",
			rightType:     "integer",
			purpose:       "o",
			sortFamSchema: "pg_catalog",
			sortFamName:   "integer_ops",
		}},
		functions: []operatorClassFunction{{
			supportNum: 1,
			fnSchema:   "app",
			fnName:     "custom_cmp",
			fnArgs:     "integer, integer",
		}},
	})
	if !ok {
		t.Fatal("expected class DDL")
	}
	wantCls := `CREATE OPERATOR CLASS "app"."custom_ops" DEFAULT FOR TYPE integer USING "btree" FAMILY "app"."custom_ops" AS OPERATOR 1 "pg_catalog".<(integer, integer) FOR ORDER BY "pg_catalog"."integer_ops", FUNCTION 1 "app"."custom_cmp"(integer, integer), STORAGE text`
	if cls != wantCls {
		t.Fatalf("class: got %q, want %q", cls, wantCls)
	}

	searchOp := formatOperatorClassOperator(operatorClassOperator{
		strategy:  1,
		opSchema:  "app",
		opName:    "!!",
		leftType:  "integer",
		rightType: "integer",
		purpose:   "s",
	})
	if searchOp != `OPERATOR 1 "app".!!(integer, integer) FOR SEARCH` {
		t.Fatalf("search operator: %q", searchOp)
	}
	add := formatAlterOperatorFamilyAdd("app", "shared", "btree", searchOp)
	wantAdd := `ALTER OPERATOR FAMILY "app"."shared" USING "btree" ADD OPERATOR 1 "app".!!(integer, integer) FOR SEARCH`
	if add != wantAdd {
		t.Fatalf("alter family: %q", add)
	}
}

func TestLoadOperatorClassesCatalog(t *testing.T) {
	src, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })

	mock.ExpectQuery(`FROM pg_opclass opc`).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "nspname", "opcname", "opcdefault", "input_type", "amname",
		"fam_nspname", "opfname", "storage_type",
	}).AddRow(42, "app", "custom_ops", true, "integer", "btree", "app", "custom_ops", ""))
	mock.ExpectQuery(`pg_amop amop`).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "strategy", "op_nspname", "oprname", "left_type", "right_type",
		"purpose", "sort_nspname", "sort_opfname",
	}).AddRow(42, 1, "pg_catalog", "<", "integer", "integer", "o", "pg_catalog", "integer_ops"))
	mock.ExpectQuery(`pg_amproc amproc`).WillReturnRows(sqlmock.NewRows([]string{
		"oid", "support", "fn_nspname", "proname", "fn_args",
	}).AddRow(42, 1, "app", "custom_cmp", "integer, integer"))

	mock.ExpectQuery(`list loose operator family operators|FROM pg_amop amop`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "strategy", "op_nspname", "oprname", "left_type", "right_type",
		"purpose", "sort_nspname", "sort_opfname",
	}))
	mock.ExpectQuery(`FROM pg_amproc amproc`).WillReturnRows(sqlmock.NewRows([]string{
		"nspname", "opfname", "amname", "support", "fn_nspname", "proname", "fn_args",
	}))

	classes, _, err := loadOperatorClasses(context.Background(), src, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) != 1 {
		t.Fatalf("classes: %+v", classes)
	}
	stmt, ok := formatCreateOperatorClass(classes[0])
	if !ok {
		t.Fatal("expected DDL")
	}
	if stmt == "" || classes[0].defaultClass != true {
		t.Fatalf("class %+v stmt %q", classes[0], stmt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
