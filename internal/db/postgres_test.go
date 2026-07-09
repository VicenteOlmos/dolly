package db

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func int64Ptr(v int64) *int64 {
	return &v
}

func TestFetchTables(t *testing.T) {
	tests := []struct {
		name     string
		rows     *sqlmock.Rows
		queryErr error
		want     []Table
		wantErr  bool
	}{
		{
			name: "row count populated",
			rows: sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
				AddRow("public", "users", int64(42)),
			want: []Table{
				{Schema: "public", Name: "users", RowCount: int64Ptr(42)},
			},
		},
		{
			name: "row count nil",
			rows: sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
				AddRow("public", "users", nil),
			want: []Table{
				{Schema: "public", Name: "users"},
			},
		},
		{
			name: "empty result",
			rows: sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}),
			want: nil,
		},
		{
			name: "rows error propagation",
			rows: sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
				AddRow("public", "users", int64(1)).
				RowError(0, errors.New("iteration failed")),
			wantErr: true,
		},
		{
			name:     "query error wrapping",
			queryErr: errors.New("query failed"),
			wantErr:  true,
		},
		{
			name: "scan error",
			rows: sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
				AddRow("public", "users", float64(1.5)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			if tt.queryErr != nil {
				mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
					WithArgs("public").
					WillReturnError(tt.queryErr)
			} else {
				mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
					WithArgs("public").
					WillReturnRows(tt.rows)
			}

			got, err := fetchTables(context.Background(), db, []string{"public"})
			if (err != nil) != tt.wantErr {
				t.Fatalf("fetchTables() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if tt.queryErr != nil && !strings.Contains(err.Error(), "fetch tables") {
					t.Fatalf("error missing expected context: %v", err)
				}
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("fetchTables() = %+v, want %+v", got, tt.want)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestLoadPostgresPublicSchema(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(sqlmock.Sqlmock)
		want    []Table
		wantErr bool
	}{
		{
			name: "full flow two tables",
			setup: func(mock sqlmock.Sqlmock) {
				tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
					AddRow("public", "users", int64(2)).
					AddRow("public", "posts", int64(0))
				mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
					WithArgs("public").
					WillReturnRows(tablesRows)

				allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
					AddRow("public", "users", "id", "integer", "NO", 1, true).
					AddRow("public", "users", "name", "text", "YES", 2, false).
					AddRow("public", "posts", "id", "integer", "NO", 1, true).
					AddRow("public", "posts", "user_id", "integer", "YES", 2, false)
				mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(allCols)

				allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}).
					AddRow("public", "posts", "fk_posts_user_id", "user_id", "public", "users", "id")
				mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(allFks)
			},
			want: []Table{
				{
					Schema:   "public",
					Name:     "users",
					RowCount: int64Ptr(2),
					Columns: []Column{
						{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1},
						{Name: "name", DataType: "text", IsNullable: true, PrimaryKey: false, OrdinalPosition: 2},
					},
					ForeignKeys: nil,
				},
				{
					Schema:   "public",
					Name:     "posts",
					RowCount: int64Ptr(0),
					Columns: []Column{
						{Name: "id", DataType: "integer", IsNullable: false, PrimaryKey: true, OrdinalPosition: 1},
						{Name: "user_id", DataType: "integer", IsNullable: true, PrimaryKey: false, OrdinalPosition: 2},
					},
					ForeignKeys: []ForeignKey{
						{ConstraintName: "fk_posts_user_id", ColumnName: "user_id", ReferencedTableSchema: "public", ReferencedTableName: "users", ReferencedColumnName: "id"},
					},
				},
			},
		},
		{
			name: "error from fetchAllColumns phase",
			setup: func(mock sqlmock.Sqlmock) {
				tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
					AddRow("public", "users", nil)
				mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
					WithArgs("public").
					WillReturnRows(tablesRows)

				mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnError(errors.New("columns query failed"))
			},
			wantErr: true,
		},
		{
			name: "error from fetchAllForeignKeys phase",
			setup: func(mock sqlmock.Sqlmock) {
				tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
					AddRow("public", "users", nil)
				mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
					WithArgs("public").
					WillReturnRows(tablesRows)

				allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
					AddRow("public", "users", "id", "integer", "NO", 1, true)
				mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(allCols)

				mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnError(errors.New("fk query failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			tt.setup(mock)

			got, err := LoadPostgresPublicSchema(context.Background(), db)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadPostgresPublicSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("LoadPostgresPublicSchema() = %+v, want %+v", got, tt.want)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestCapabilityBoundary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*table_type = 'BASE TABLE'[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	got, err := LoadPostgresPublicSchema(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 table, got %d", len(got))
	}
	if got[0].Schema != "public" || got[0].Name != "users" {
		t.Fatalf("unexpected table: %+v", got[0])
	}
	if len(got[0].Columns) != 1 || got[0].Columns[0].Name != "id" {
		t.Fatalf("unexpected columns: %+v", got[0].Columns)
	}
	if len(got[0].ForeignKeys) != 0 {
		t.Fatalf("expected no foreign keys, got %+v", got[0].ForeignKeys)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadPostgresSchemasEmptyUsesPublicOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("public", "users", int64(1))
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("public").
		WillReturnRows(tablesRows)

	colsRows := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("public", "users", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("public").WillReturnRows(colsRows)

	fksRows := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("public").WillReturnRows(fksRows)

	got, err := LoadPostgresSchemas(context.Background(), db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Schema != "public" {
		t.Fatalf("got %+v, want public users", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadPostgresSchemasMultiSchemaIN(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "orders", int64(3)).
		AddRow("billing", "invoices", nil)
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("app", "billing").
		WillReturnRows(tablesRows)

	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "orders", "id", "integer", "NO", 1, true).
		AddRow("billing", "invoices", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`SELECT c\.table_schema`).WithArgs("app", "billing").WillReturnRows(allCols)
	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"})
	mock.ExpectQuery(`SELECT tc\.table_schema`).WithArgs("app", "billing").WillReturnRows(allFks)

	got, err := LoadPostgresSchemas(context.Background(), db, []string{"app", "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tables, want 2", len(got))
	}
	if got[0].Schema != "app" || got[1].Schema != "billing" {
		t.Fatalf("unexpected schemas: %+v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadPostgresSchemasUnknownSchemaReturnsEmpty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("ghost").
		WillReturnRows(tablesRows)

	got, err := LoadPostgresSchemas(context.Background(), db, []string{"ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unknown schema should yield no tables, got %+v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadPostgresSchemasBatched(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Two tables across two schemas.
	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"}).
		AddRow("app", "orders", int64(3)).
		AddRow("billing", "invoices", nil)
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name, s\.n_live_tup[\s\S]*table_schema IN \(\$1, \$2\)[\s\S]*ORDER BY t\.table_schema, t\.table_name`).
		WithArgs("app", "billing").
		WillReturnRows(tablesRows)

	allCols := sqlmock.NewRows([]string{"table_schema", "table_name", "column_name", "data_type", "is_nullable", "ordinal_position", "is_primary_key"}).
		AddRow("app", "orders", "id", "integer", "NO", 1, true).
		AddRow("app", "orders", "amount", "numeric", "YES", 2, false).
		AddRow("billing", "invoices", "id", "integer", "NO", 1, true)
	mock.ExpectQuery(`FROM information_schema.columns c[\s\S]*ORDER BY c\.table_schema, c\.table_name, c\.ordinal_position`).
		WithArgs("app", "billing").
		WillReturnRows(allCols)

	allFks := sqlmock.NewRows([]string{"table_schema", "table_name", "constraint_name", "column_name", "ccu.table_schema", "ccu.table_name", "ccu.column_name"}).
		AddRow("billing", "invoices", "fk_inv_order", "order_id", "app", "orders", "id")
	mock.ExpectQuery(`tc\.constraint_type = 'FOREIGN KEY'[\s\S]*table_schema IN \(\$1, \$2\)[\s\S]*ORDER BY tc\.table_schema, tc\.table_name, tc\.constraint_name, kcu\.column_name`).
		WithArgs("app", "billing").
		WillReturnRows(allFks)

	got, err := LoadPostgresSchemasBatched(context.Background(), db, []string{"app", "billing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tables, want 2", len(got))
	}
	if got[0].Schema != "app" || got[0].Name != "orders" {
		t.Fatalf("table[0] = %s.%s, want app.orders", got[0].Schema, got[0].Name)
	}
	if len(got[0].Columns) != 2 {
		t.Fatalf("orders: got %d columns, want 2", len(got[0].Columns))
	}
	if len(got[1].ForeignKeys) != 1 || got[1].ForeignKeys[0].ConstraintName != "fk_inv_order" {
		t.Fatalf("invoices: unexpected FKs: %+v", got[1].ForeignKeys)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoadPostgresSchemasBatchedEmpty(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tablesRows := sqlmock.NewRows([]string{"table_schema", "table_name", "n_live_tup"})
	mock.ExpectQuery(`SELECT t\.table_schema, t\.table_name`).
		WithArgs("ghost").
		WillReturnRows(tablesRows)

	got, err := LoadPostgresSchemasBatched(context.Background(), db, []string{"ghost"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d tables", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
