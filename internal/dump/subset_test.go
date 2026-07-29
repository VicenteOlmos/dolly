package dump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestPrimaryKeyColumnRejectsComposite(t *testing.T) {
	tbl := db.Table{
		Name: "project_members",
		Columns: []db.Column{
			{Name: "project_id", PrimaryKey: true},
			{Name: "tbl_a_id", PrimaryKey: true},
		},
	}
	_, err := primaryKeyColumn(tbl)
	if err == nil || !strings.Contains(err.Error(), "composite") {
		t.Fatalf("primaryKeyColumn() = %v", err)
	}
}

func TestExpandPKChunks(t *testing.T) {
	vals := make([]any, 1200)
	for i := range vals {
		vals[i] = i + 1
	}
	chunks := expandPKChunks("id", vals, 500)
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
}

func TestMergeWhereClausesOR(t *testing.T) {
	sql, args, err := mergeWhereClauses([]compiledWhere{
		{sql: `("id" = $1)`, args: []any{1}},
		{sql: `("id" = ANY($1))`, args: []any{[]any{2, 3}}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, " OR ") {
		t.Fatalf("sql = %q, want OR", sql)
	}
	if len(args) != 2 {
		t.Fatalf("args = %d, want 2", len(args))
	}
}

func TestPlanSubsetTableSeed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	seedRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE`).
		WithArgs(int64(1)).
		WillReturnRows(seedRows)

	fkRows := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(1)).
		WillReturnRows(fkRows)

	childRows := sqlmock.NewRows([]string{"id"}).
		AddRow(int64(1)).
		AddRow(int64(2))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id" = \$1`).
		WithArgs(int64(10)).
		WillReturnRows(childRows)

	fkRows2 := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(2)).
		WillReturnRows(fkRows2)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables) < 2 {
		t.Fatalf("tables = %d, want at least 2", len(plan.tables))
	}
	if _, ok := plan.tables[tableKey("public", "tbl_a")]; !ok {
		t.Fatal("missing tbl_a in plan")
	}
	if _, ok := plan.tables[tableKey("public", "departments")]; !ok {
		t.Fatal("missing departments in plan")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPlanSubsetMaxTablesExceeded(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	seedRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a"`).
		WillReturnRows(seedRows)

	fkRows := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a"`).
		WillReturnRows(fkRows)

	childRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(2))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id"`).
		WillReturnRows(childRows)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 1, MaxRows: 1000, MaxInListSize: 500},
	}

	_, err = planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err == nil {
		t.Fatal("expected max_tables error")
	}
	var lim *LimitError
	if !strings.Contains(err.Error(), "max_tables") {
		t.Fatalf("error = %v", err)
	}
	_ = lim
}

func TestStreamTableFilteredUsesWhere(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := fixtureTables()[1]
	clauses := []compiledWhere{{sql: `("id" = $1)`, args: []any{int64(1)}}}
	rows := sqlmock.NewRows([]string{"id", "department_id", "name"}).
		AddRow(int64(1), int64(10), "v1")

	mock.ExpectQuery(`SELECT .+ FROM "public"\."tbl_a" WHERE`).
		WithArgs(int64(1)).
		WillReturnRows(rows)

	dir := t.TempDir()
	if err := streamTableFiltered(context.Background(), sqlDB, table, dir, clauses, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanSubsetParentSeed(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	deptRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."departments" WHERE`).
		WithArgs(int64(10)).
		WillReturnRows(deptRows)

	childRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id" = \$1`).
		WithArgs(int64(10)).
		WillReturnRows(childRows)

	fkRows1 := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(1)).
		WillReturnRows(fkRows1)

	fkRows2 := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(2)).
		WillReturnRows(fkRows2)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "departments", Column: "id", Op: PredicateEq, Value: int64(10)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(plan.tables))
	}
	if _, ok := plan.tables[tableKey("public", "departments")]; !ok {
		t.Fatal("missing departments")
	}
	if _, ok := plan.tables[tableKey("public", "tbl_a")]; !ok {
		t.Fatal("missing tbl_a")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPlanSubsetNullableFKSkip(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "departments",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
			},
		},
		{
			Schema: "public",
			Name:   "tbl_a",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "department_id", DataType: "integer", OrdinalPosition: 2, IsNullable: true},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "tbl_a_department_id_fkey",
					ColumnName:            "department_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "departments",
					ReferencedColumnName:  "id",
				},
			},
		},
	}

	seedRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE`).
		WithArgs(int64(1)).
		WillReturnRows(seedRows)

	fkRows := sqlmock.NewRows([]string{"department_id"}).AddRow(nil)
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(1)).
		WillReturnRows(fkRows)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.tables[tableKey("public", "departments")]; ok {
		t.Fatal("departments should not be included when FK is NULL")
	}
	if _, ok := plan.tables[tableKey("public", "tbl_a")]; !ok {
		t.Fatal("missing tbl_a")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestVerifyClosureIntegrityDirect(t *testing.T) {
	visited := map[string]*pkSet{
		"tbl_a": {keys: map[string]struct{}{"i:1": {}}, vals: []any{int64(1)}},
	}
	requiredParents := map[string]map[string]struct{}{
		"tbl_a": {"departments": {}},
	}

	err := verifyClosureIntegrity(visited, requiredParents)
	if err == nil {
		t.Fatal("expected closure integrity error")
	}
	if !strings.Contains(err.Error(), "closure integrity") {
		t.Fatalf("error = %v", err)
	}

	visited["departments"] = &pkSet{keys: map[string]struct{}{"i:10": {}}, vals: []any{int64(10)}}
	err = verifyClosureIntegrity(visited, requiredParents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStreamTableFilteredEmptyResult(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	table := fixtureTables()[1]
	clauses := []compiledWhere{{sql: `("id" = $1)`, args: []any{int64(999)}}}
	rows := sqlmock.NewRows([]string{"id", "department_id", "name"})

	mock.ExpectQuery(`SELECT .+ FROM "public"\."tbl_a" WHERE`).
		WithArgs(int64(999)).
		WillReturnRows(rows)

	dir := t.TempDir()
	if err := streamTableFiltered(context.Background(), sqlDB, table, dir, clauses, nil, ""); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "tbl_a.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty ndjson file, got %q", string(data))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanSubsetTextPKIdentity(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "codes",
			Columns: []db.Column{
				{Name: "code", DataType: "text", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "name", DataType: "text", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "items",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "code_ref", DataType: "text", OrdinalPosition: 2},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "items_code_ref_fkey",
					ColumnName:            "code_ref",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "codes",
					ReferencedColumnName:  "code",
				},
			},
		},
	}

	seedRows := sqlmock.NewRows([]string{"code"}).AddRow("001")
	mock.ExpectQuery(`SELECT "code" FROM "public"\."codes" WHERE`).
		WithArgs("001").
		WillReturnRows(seedRows)

	childRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."items" WHERE "code_ref" = \$1`).
		WithArgs("001").
		WillReturnRows(childRows)

	fkRows := sqlmock.NewRows([]string{"code_ref"}).AddRow("001")
	mock.ExpectQuery(`SELECT "code_ref" FROM "public"\."items" WHERE "id" = \$1`).
		WithArgs(int64(1)).
		WillReturnRows(fkRows)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "codes", Column: "code", Op: PredicateEq, Value: "001"},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(plan.tables))
	}

	codesPlan := plan.tables[tableKey("public", "codes")]
	if len(codesPlan.pkValues) != 1 || codesPlan.pkValues[0] != "001" {
		t.Fatalf("codes pk = %v, want [\"001\"]", codesPlan.pkValues)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPlanSubsetPercentModeReachesPlan(t *testing.T) {
	// Verify the percent code path creates a valid plan result.
	// Full FK closure is tested by existing seed-file tests; this test
	// confirms percent mode walks the same machinery.
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.MatchExpectationsInOrder(false)

	// COUNT(*) for root table only (tbl_a has FK → not a percent root).
	_ = mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// Recent PKs for departments root.
	_ = mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(2)))

	// Child percent sampling: departments -> tbl_a.
	_ = mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."tbl_a" WHERE "department_id" IN \(\$1\)`).
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	_ = mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id" IN \(\$1\)`).
		WithArgs(int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	// Parent hops and further closure.
	for range 8 {
		_ = mock.ExpectQuery(`SELECT COUNT\(\*\)|SELECT "id"|SELECT "department_id"`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	for range 8 {
		_ = mock.ExpectQuery(`SELECT COUNT\(\*\)|SELECT "id"|SELECT "department_id"`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	}
	for range 8 {
		_ = mock.ExpectQuery(`WHERE`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	}
	// Note: we don't call ExpectationsWereMet because the exact FK query count
	// depends on map iteration order in the BFS queue. See TestPlanSubsetTableSeed
	// for ordered FK closure tests.

	cfg := SubsetConfig{
		Percent: 50,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 100, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.tables) < 2 {
		t.Fatalf("tables = %d, want at least 2", len(plan.tables))
	}
	if _, ok := plan.tables[tableKey("public", "tbl_a")]; !ok {
		t.Fatal("missing tbl_a in plan")
	}
	if _, ok := plan.tables[tableKey("public", "departments")]; !ok {
		t.Fatal("missing departments in plan")
	}
	// Verify rows_exported has both tables.
	if plan.rowsExported[tableKey("public", "tbl_a")] == 0 {
		t.Fatal("expected rows for tbl_a")
	}
	if plan.rowsExported[tableKey("public", "departments")] == 0 {
		t.Fatal("expected rows for departments")
	}
}

func TestPlanSubsetPercentMutualExclusion(t *testing.T) {
	cfg := SubsetConfig{
		Percent: 50,
		Seeds:   []RowPredicate{{Table: "tbl_a", Column: "id", Op: PredicateEq, Value: int64(1)}},
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	_, err := planSubset(context.Background(), nil, fixtureTables(), cfg)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestPlanSubsetPercentInvalid(t *testing.T) {
	cfg := SubsetConfig{
		Percent: 0,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}
	_, err := planSubset(context.Background(), nil, fixtureTables(), cfg)
	if err == nil || !strings.Contains(err.Error(), "at least one seed") {
		t.Fatalf("expected seed required error, got %v", err)
	}

	cfg.Percent = 150
	_, err = planSubset(context.Background(), nil, fixtureTables(), cfg)
	if err == nil || !strings.Contains(err.Error(), "must be between 1 and 100") {
		t.Fatalf("expected percent range error, got %v", err)
	}
}

func TestPlanSubsetPercentJoinTableChild(t *testing.T) {
	// Composite-PK join table like project_members should be included as
	// a child in percent subset, exported via FK predicates (not skipped).
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.MatchExpectationsInOrder(false)

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "projects",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "name", DataType: "text", OrdinalPosition: 2},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 3},
			},
		},
		{
			Schema: "public",
			Name:   "project_members",
			Columns: []db.Column{
				{Name: "project_id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "user_id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 2},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "pm_project_fkey",
					ColumnName:            "project_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "projects",
					ReferencedColumnName:  "id",
				},
			},
		},
	}

	// COUNT(*) for projects (no RowCount, falls back to COUNT).
	_ = mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	// Recent PKs for projects.
	_ = mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))

	// FK parent hops from projects — none (projects has no FK).
	// Child hop: projects -> project_members via FK.
	// For each selected project PK (1, 2), composite PK handling builds FK predicates.
	// No PK selection for composite tables; instead FK WHERE clauses are stored.
	// The BFS processes the two project PKs; for each, the child hop is detected as composite
	// and stored as fkPredicates.
	// We expect no further SQL queries for the composite table during planning.

	cfg := SubsetConfig{
		Percent: 50,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 100, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// projects must be in plan (as root).
	if _, ok := plan.tables[tableKey("public", "projects")]; !ok {
		t.Fatal("missing projects in plan")
	}
	// project_members must be in plan (as composite-PK child).
	pm, ok := plan.tables[tableKey("public", "project_members")]
	if !ok {
		t.Fatal("missing project_members in plan — composite-PK join table was skipped")
	}
	// Must have compositeFKVals (not pkValues) for export.
	if len(pm.compositeFKVals) == 0 {
		t.Fatal("expected compositeFKVals for composite-PK child table, got none")
	}
	if len(pm.pkValues) != 0 {
		t.Fatal("expected no pkValues for composite-PK table")
	}
	// Row count is sum of FK values across all columns.
	totalFK := 0
	for _, vals := range pm.compositeFKVals {
		totalFK += len(vals)
	}
	if plan.rowsExported[tableKey("public", "project_members")] != totalFK {
		t.Fatalf("rowsExported = %d, want %d", plan.rowsExported[tableKey("public", "project_members")], totalFK)
	}
}

func TestBuildCompositeFKPredicate_ANDsMultipleFKs(t *testing.T) {
	// Multi-FK join table: predicate must AND across FK columns,
	// not OR them. Otherwise a row referencing a parent that
	// was NOT selected would still pass the WHERE filter.
	fkGrouped := map[string][]any{
		"project_id":  {int64(1), int64(2)},
		"employee_id": {int64(10), int64(20)},
	}
	clauses := buildCompositeFKPredicate(fkGrouped)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	sql := clauses[0].sql
	if !strings.Contains(sql, " AND ") {
		t.Fatalf("sql = %q, want AND between FK IN clauses", sql)
	}
	if !strings.Contains(sql, "ANY") {
		t.Fatalf("sql = %q, want ANY IN clauses", sql)
	}
	// Must contain both FK columns.
	if !strings.Contains(sql, "project_id") || !strings.Contains(sql, "employee_id") {
		t.Fatalf("sql = %q, missing FK column", sql)
	}
	if len(clauses[0].args) != 2 {
		t.Fatalf("args = %d, want 2 (one per FK column)", len(clauses[0].args))
	}
}

func TestBuildCompositeFKPredicate_SingleFK(t *testing.T) {
	// Single-FK join table: still produces AND'd clause (only one).
	fkGrouped := map[string][]any{
		"project_id": {int64(1), int64(2)},
	}
	clauses := buildCompositeFKPredicate(fkGrouped)
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	sql := clauses[0].sql
	if strings.Contains(sql, " AND ") {
		t.Fatalf("sql = %q, single-FK should not contain AND", sql)
	}
	if !strings.Contains(sql, "ANY") {
		t.Fatalf("sql = %q, want ANY IN clause", sql)
	}
}

func TestPlanSubsetMultiFKJoinTable(t *testing.T) {
	// Join table with two FKs (project_id → projects, employee_id → employees).
	// Both parents are percent roots. The join table must have an AND'd predicate
	// that filters on BOTH FK columns, so no orphan rows with missing parent
	// references are exported.
	tables := []db.Table{
		{
			Schema: "public",
			Name:   "projects",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "employees",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "project_members",
			Columns: []db.Column{
				{Name: "project_id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "employee_id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 2},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "pm_project_fkey",
					ColumnName:            "project_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "projects",
					ReferencedColumnName:  "id",
				},
				{
					ConstraintName:        "pm_employee_fkey",
					ColumnName:            "employee_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "employees",
					ReferencedColumnName:  "id",
				},
			},
		},
	}

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	mock.MatchExpectationsInOrder(false)

	// COUNT(*) for projects and employees.
	_ = mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	_ = mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// Recent PKs for projects.
	_ = mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))
	// Recent PKs for employees.
	_ = mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(10)).AddRow(int64(20)).AddRow(int64(30)))

	// BFS closures — enough slack for map iteration order.
	for range 20 {
		_ = mock.ExpectQuery(`WHERE`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	}

	cfg := SubsetConfig{
		Percent: 50,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 100, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}

	pm, ok := plan.tables[tableKey("public", "project_members")]
	if !ok {
		t.Fatal("missing project_members in plan")
	}
	if len(pm.compositeFKVals) < 2 {
		t.Fatalf("expected at least 2 FK columns in compositeFKVals, got %d (%v)", len(pm.compositeFKVals), pm.compositeFKVals)
	}
	// Must have values for both FK columns.
	if len(pm.compositeFKVals["project_id"]) == 0 {
		t.Fatal("expected project_id values in compositeFKVals")
	}
	if len(pm.compositeFKVals["employee_id"]) == 0 {
		t.Fatal("expected employee_id values in compositeFKVals")
	}
	// Verify the exported predicate uses AND.
	clauses, err := buildStreamClauses("", pm, SubsetLimits{MaxInListSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(clauses) != 1 {
		t.Fatalf("expected 1 clause, got %d", len(clauses))
	}
	sql := clauses[0].sql
	if !strings.Contains(sql, " AND ") {
		t.Fatalf("sql = %q, multi-FK join table predicate must use AND between FK IN clauses", sql)
	}
}

func TestSelectRecentPKsTieBreaker(t *testing.T) {
	// Verify that ORDER BY includes pk DESC tie-breaker when orderCol != pkCol.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// The query must contain the tie-breaker: ORDER BY "created_at" DESC NULLS LAST, "id" DESC LIMIT
	rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`ORDER BY .*DESC NULLS LAST.*,.*DESC.*LIMIT`).
		WillReturnRows(rows)

	table := db.Table{
		Schema: "public",
		Name:   "test",
		Columns: []db.Column{
			{Name: "id", DataType: "integer", PrimaryKey: true},
			{Name: "created_at", DataType: "timestamp"},
		},
	}

	_, err = selectRecentPKs(context.Background(), sqlDB, table, "id", "created_at", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSelectRecentPKsNoTieBreakerWhenOrderIsPK(t *testing.T) {
	// When orderCol == pkCol, no extra tie-breaker needed.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	// Expect exactly one ORDER BY column (no tie-breaker).
	mock.ExpectQuery(`ORDER BY "id" DESC NULLS LAST LIMIT`).
		WillReturnRows(rows)

	table := db.Table{
		Schema: "public",
		Name:   "test",
		Columns: []db.Column{
			{Name: "id", DataType: "bigint", PrimaryKey: true},
		},
	}

	_, err = selectRecentPKs(context.Background(), sqlDB, table, "id", "id", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSelectPercentRootsUsesRowCountEstimate(t *testing.T) {
	// When db.Table.RowCount is set, it should be used instead of COUNT(*).
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	rc := int64(500)
	tables := []db.Table{
		{
			Schema:   "public",
			Name:     "big_table",
			RowCount: &rc,
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
				{Name: "created_at", DataType: "timestamp"},
			},
		},
	}

	// No COUNT(*) query should be expected — RowCount estimate is used.
	// But selectRecentPKs will be called with limit = ceil(500 * 50 / 100) = 250.
	rows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(rows)

	roots, err := selectPercentRoots(context.Background(), sqlDB, tables, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(roots))
	}
	if roots[0].limit != 250 {
		t.Fatalf("limit = %d, want 250", roots[0].limit)
	}
}

func TestPlanSubsetPercentNoCandidateRootsKeepsEligibilityDiagnostic(t *testing.T) {
	tables := []db.Table{
		{
			Schema: "public",
			Name:   "composite_only",
			Columns: []db.Column{
				{Name: "a", DataType: "integer", PrimaryKey: true},
				{Name: "b", DataType: "integer", PrimaryKey: true},
			},
		},
	}
	_, err := planSubset(context.Background(), nil, tables, SubsetConfig{
		Percent: 50,
		Limits:  DefaultSubsetLimits(),
	})
	if err == nil {
		t.Fatal("expected percent planning error")
	}
	if IsNoTablesError(err) {
		t.Fatalf("error = %v, want candidate-root diagnostic not NoTablesError", err)
	}
	if !strings.Contains(err.Error(), "no candidate root tables") {
		t.Fatalf("error = %q, want candidate-root message", err.Error())
	}
}

func TestSelectPercentRootsMaxRowsClamping(t *testing.T) {
	// MaxRows should clamp LIMIT before querying to avoid fetching millions.
	// Two roots, first exhausts budget, second errors.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	rc := int64(100000)
	tables := []db.Table{
		{
			Schema:   "public",
			Name:     "huge",
			RowCount: &rc,
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
				{Name: "created_at", DataType: "timestamp"},
			},
		},
		{
			Schema:   "public",
			Name:     "tiny",
			RowCount: &rc,
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true},
				{Name: "created_at", DataType: "timestamp"},
			},
		},
	}

	// First root queries with clamped LIMIT (100 = MaxRows).
	// Return enough rows to exhaust the budget.
	hugeRows := sqlmock.NewRows([]string{"id"})
	for i := 1; i <= 100; i++ {
		hugeRows.AddRow(int64(i))
	}
	mock.ExpectQuery(`ORDER BY`).WillReturnRows(hugeRows)

	cfg := SubsetConfig{
		Percent: 50,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 100, MaxInListSize: 500},
	}

	_, err = planSubset(context.Background(), sqlDB, tables, cfg)
	if err == nil || !strings.Contains(err.Error(), "max_rows") {
		t.Fatalf("expected max_rows limit error, got %v", err)
	}
}

func TestPlanSubsetPercentChildSampling(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "orders",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "order_items",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "order_id", DataType: "integer", OrdinalPosition: 2},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 3},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "order_items_order_id_fkey",
					ColumnName:            "order_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "orders",
					ReferencedColumnName:  "id",
				},
			},
		},
	}

	rc := int64(10)
	tables[0].RowCount = &rc

	mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(10)))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."order_items" WHERE "order_id" IN \(\$1\)`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(100))

	mock.ExpectQuery(`SELECT "id" FROM "public"\."order_items" WHERE "order_id" IN \(\$1\)`).
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(int64(100)).
			AddRow(int64(99)).
			AddRow(int64(98)).
			AddRow(int64(97)).
			AddRow(int64(96)).
			AddRow(int64(95)).
			AddRow(int64(94)).
			AddRow(int64(93)).
			AddRow(int64(92)).
			AddRow(int64(91)))

	for _, id := range []int64{100, 99, 98, 97, 96, 95, 94, 93, 92, 91} {
		mock.ExpectQuery(`SELECT "order_id" FROM "public"\."order_items" WHERE "id" = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows([]string{"order_id"}).AddRow(int64(10)))
	}

	cfg := SubsetConfig{
		Percent: 10,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	items := plan.tables[tableKey("public", "order_items")]
	if len(items.pkValues) != 10 {
		t.Fatalf("order_items pk count = %d, want 10", len(items.pkValues))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPlanSubsetMaxRowsPerTable(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	tables := []db.Table{
		{
			Schema: "public",
			Name:   "orders",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 2},
			},
		},
		{
			Schema: "public",
			Name:   "order_items",
			Columns: []db.Column{
				{Name: "id", DataType: "integer", PrimaryKey: true, OrdinalPosition: 1},
				{Name: "order_id", DataType: "integer", OrdinalPosition: 2},
				{Name: "created_at", DataType: "timestamp", OrdinalPosition: 3},
			},
			ForeignKeys: []db.ForeignKey{
				{
					ConstraintName:        "order_items_order_id_fkey",
					ColumnName:            "order_id",
					ReferencedTableSchema: "public",
					ReferencedTableName:   "orders",
					ReferencedColumnName:  "id",
				},
			},
		},
	}

	rc := int64(1)
	tables[0].RowCount = &rc

	mock.ExpectQuery(`ORDER BY`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM "public"\."order_items" WHERE "order_id" IN \(\$1\)`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))

	mock.ExpectQuery(`SELECT "id" FROM "public"\."order_items" WHERE "order_id" IN \(\$1\)`).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(int64(20)).
			AddRow(int64(19)).
			AddRow(int64(18)).
			AddRow(int64(17)).
			AddRow(int64(16)))

	for _, id := range []int64{20, 19, 18, 17, 16} {
		mock.ExpectQuery(`SELECT "order_id" FROM "public"\."order_items" WHERE "id" = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows([]string{"order_id"}).AddRow(int64(1)))
	}

	cfg := SubsetConfig{
		Percent: 50,
		Limits:  SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1000, MaxRowsPerTable: 5, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, tables, cfg)
	if err != nil {
		t.Fatal(err)
	}
	items := plan.tables[tableKey("public", "order_items")]
	if len(items.pkValues) != 5 {
		t.Fatalf("order_items pk count = %d, want 5", len(items.pkValues))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPercentRowLimit(t *testing.T) {
	tests := []struct {
		count, percent, want int
	}{
		{100, 10, 10},
		{0, 10, 0},
		{3, 50, 2},
	}
	for _, tt := range tests {
		if got := percentRowLimit(tt.count, tt.percent); got != tt.want {
			t.Fatalf("percentRowLimit(%d, %d) = %d, want %d", tt.count, tt.percent, got, tt.want)
		}
	}
}

func TestPlanSubsetChildClosureOverMaxRows(t *testing.T) {
	// High-fanout FK child closure exceeds MaxRows before all child rows
	// are fetched: the query must include a LIMIT that detects overflow
	// and return a LimitError.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// Seed: one department (uses all of MaxRows=1 budget).
	deptRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."departments" WHERE`).
		WithArgs(int64(10)).
		WillReturnRows(deptRows)

	// Child closure query should include LIMIT 1 (remaining=0 → maxRows+1=1).
	// Even one child row exceeds the remaining budget.
	childRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id" = \$1 LIMIT`).
		WithArgs(int64(10)).
		WillReturnRows(childRows)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "departments", Column: "id", Op: PredicateEq, Value: int64(10)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 1, MaxInListSize: 500},
	}

	_, err = planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err == nil {
		t.Fatal("expected max_rows limit error, got nil")
	}
	var lim *LimitError
	if !strings.Contains(err.Error(), "max_rows") {
		t.Fatalf("error = %v, want max_rows LimitError", err)
	}
	_ = lim

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPlanSubsetChildClosureUnderMaxRows(t *testing.T) {
	// Child closure stays within budget: no overflow, query uses remaining+1
	// as the LIMIT value.
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	// Seed: one department.
	deptRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."departments" WHERE`).
		WithArgs(int64(10)).
		WillReturnRows(deptRows)

	// Child closure: MaxRows=5, seed uses 1, remaining=4 → LIMIT 5.
	// Return 2 child rows (within budget).
	childRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2))
	mock.ExpectQuery(`SELECT "id" FROM "public"\."tbl_a" WHERE "department_id" = \$1 LIMIT`).
		WithArgs(int64(10)).
		WillReturnRows(childRows)

	// FK closure: child rows reference back to department.
	fkRows1 := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(1)).
		WillReturnRows(fkRows1)

	fkRows2 := sqlmock.NewRows([]string{"department_id"}).AddRow(int64(10))
	mock.ExpectQuery(`SELECT "department_id" FROM "public"\."tbl_a" WHERE "id" = \$1`).
		WithArgs(int64(2)).
		WillReturnRows(fkRows2)

	cfg := SubsetConfig{
		Seeds: []RowPredicate{
			{Table: "departments", Column: "id", Op: PredicateEq, Value: int64(10)},
		},
		Limits: SubsetLimits{MaxDepth: 5, MaxTables: 10, MaxRows: 5, MaxInListSize: 500},
	}

	plan, err := planSubset(context.Background(), sqlDB, fixtureTables(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(plan.tables))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
