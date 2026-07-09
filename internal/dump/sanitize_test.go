package dump

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/DATA-DOG/go-sqlmock"
)

func TestSanitizeByPattern(t *testing.T) {
	columns := []db.Column{
		{Name: "id", DataType: "integer"},
		{Name: "name", DataType: "text"},
		{Name: "email", DataType: "character varying"},
		{Name: "password", DataType: "text"},
		{Name: "ssn", DataType: "varchar"},
		{Name: "phone", DataType: "text"},
		{Name: "token", DataType: "integer"},
		{Name: "api_key", DataType: "text"},
	}

	tests := []struct {
		name string
		row  map[string]any
		want map[string]any
	}{
		{
			name: "sensitive text columns replaced",
			row: map[string]any{
				"id": 1, "name": "Alice",
				"email": "alice@corp.test", "password": "secret",
				"ssn": "123-45-6789", "phone": "555-1234",
				"token": 42, "api_key": "sk-live",
			},
			want: map[string]any{
				"id": 1, "name": "Alice",
				"email": "redacted@example.com", "password": "[REDACTED]",
				"ssn": "000-00-0000", "phone": "+1-555-000-0000",
				// token is integer → nil (non-text sensitive column)
				"token": nil, "api_key": "[REDACTED]",
			},
		},
		{
			name: "null email preserved",
			row: map[string]any{
				"id": 2, "name": "Bob", "email": nil,
				"password": nil, "ssn": nil, "phone": nil,
				"token": nil, "api_key": nil,
			},
			want: map[string]any{
				"id": 2, "name": "Bob", "email": nil,
				"password": nil, "ssn": nil, "phone": nil,
				"token": nil, "api_key": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := copyRow(tt.row)
			got, err := SanitizeByPattern("public", "users", columns, row)
			if err != nil {
				t.Fatal(err)
			}
			for k, wantVal := range tt.want {
				if got[k] != wantVal {
					t.Fatalf("column %q: got %v (%T), want %v (%T)", k, got[k], got[k], wantVal, wantVal)
				}
			}
		})
	}
}

func TestSanitizeByPatternCaseInsensitiveColumnName(t *testing.T) {
	columns := []db.Column{
		{Name: "EMAIL", DataType: "text"},
	}
	row := map[string]any{"EMAIL": "c@test.com"}
	got, err := SanitizeByPattern("public", "users", columns, row)
	if err != nil {
		t.Fatal(err)
	}
	if got["EMAIL"] != "redacted@example.com" {
		t.Fatalf("EMAIL = %v", got["EMAIL"])
	}
}

func TestSanitizeByPatternCreditCard(t *testing.T) {
	columns := []db.Column{{Name: "credit_card", DataType: "text"}}
	row := map[string]any{"credit_card": "4111-1111-1111-1111"}
	got, err := SanitizeByPattern("public", "cards", columns, row)
	if err != nil {
		t.Fatal(err)
	}
	if got["credit_card"] != "xxxx-xxxx-xxxx-0000" {
		t.Fatalf("credit_card = %v", got["credit_card"])
	}
}

func TestSanitizeByPatternNewExactNames(t *testing.T) {
	tests := []struct {
		colName    string
		dataType   string
		value      any
		want       any
	}{
		{"user_password", "text", "hunter2", "[REDACTED]"},
		{"auth_token", "text", "tok123", "[REDACTED]"},
		{"refresh_token", "text", "ref456", "[REDACTED]"},
		{"bearer_token", "text", "bear789", "[REDACTED]"},
		{"private_key", "text", "-----BEGIN", "[REDACTED]"},
		{"cvv", "text", "123", "[REDACTED]"},
		{"cvv_code", "text", "456", "[REDACTED]"},
		{"iban", "text", "GB82", "[REDACTED]"},
		{"salt", "text", "randomsalt", "[REDACTED]"},
		{"mfa_code", "text", "123456", "[REDACTED]"},
		{"totp_secret", "text", "JBSWY3DPEHPK3PXP", "[REDACTED]"},
		{"recovery_code", "text", "rec123", "[REDACTED]"},
		{"backup_code", "text", "bup456", "[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.colName, func(t *testing.T) {
			columns := []db.Column{{Name: tt.colName, DataType: tt.dataType}}
			row := map[string]any{tt.colName: tt.value}
			got, err := SanitizeByPattern("public", "t", columns, row)
			if err != nil {
				t.Fatal(err)
			}
			if got[tt.colName] != tt.want {
				t.Fatalf("%s = %v (%T), want %v", tt.colName, got[tt.colName], got[tt.colName], tt.want)
			}
		})
	}
}

func TestSanitizeByPatternSubstringMatch(t *testing.T) {
	tests := []struct {
		colName  string
		dataType string
		value    any
		want     any
	}{
		{"api_token", "text", "s3cr3t", "[REDACTED]"},
		{"client_secret", "text", "shhh", "[REDACTED]"},
		{"password_reset_token", "text", "tok", "[REDACTED]"},
		{"my_passwd", "text", "x", "[REDACTED]"},
		{"some_api_key", "text", "abc", "[REDACTED]"},
		{"my_private_key", "text", "pem", "[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.colName, func(t *testing.T) {
			columns := []db.Column{{Name: tt.colName, DataType: tt.dataType}}
			row := map[string]any{tt.colName: tt.value}
			got, err := SanitizeByPattern("public", "t", columns, row)
			if err != nil {
				t.Fatal(err)
			}
			if got[tt.colName] != tt.want {
				t.Fatalf("%s = %v (%T), want %v", tt.colName, got[tt.colName], got[tt.colName], tt.want)
			}
		})
	}
}

func TestSanitizeByPatternNonTextTypes(t *testing.T) {
	// Non-text columns matching sensitive names get nil'd.
	tests := []struct {
		colName  string
		dataType string
		value    any
		want     any
	}{
		{"secret", "bytea", []byte{0xde, 0xad}, nil},
		{"api_key", "uuid", "550e8400-e29b-41d4-a716-446655440000", nil},
		{"api_token", "numeric", "123.45", nil},
		{"credit_card", "bigint", int64(4111111111111111), nil},
		{"ssn", "jsonb", map[string]any{"val": "123"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.colName+"/"+tt.dataType, func(t *testing.T) {
			columns := []db.Column{{Name: tt.colName, DataType: tt.dataType}}
			row := map[string]any{tt.colName: tt.value}
			got, err := SanitizeByPattern("public", "t", columns, row)
			if err != nil {
				t.Fatal(err)
			}
			if got[tt.colName] != tt.want {
				t.Fatalf("%s = %v (%T), want %v (%T)", tt.colName, got[tt.colName], got[tt.colName], tt.want, tt.want)
			}
		})
	}
}

func TestSanitizationOptionsDisabled(t *testing.T) {
	if opts := SanitizationOptions(false); len(opts) != 0 {
		t.Fatalf("expected no options, got %d", len(opts))
	}
}

func TestStreamTableSanitizedVsPlain(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
			{Name: "email", DataType: "text"},
		},
	}
	rows := sqlmock.NewRows([]string{"id", "name", "email"}).
		AddRow(1, "Alice", "alice@corp.test")

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(rows)

	plainDir := t.TempDir()
	if err := streamTable(context.Background(), sqlDB, table, plainDir, nil); err != nil {
		t.Fatal(err)
	}
	plain, err := os.ReadFile(filepath.Join(plainDir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	rows2 := sqlmock.NewRows([]string{"id", "name", "email"}).
		AddRow(1, "Alice", "alice@corp.test")
	mock.ExpectQuery("SELECT .* FROM .*").WillReturnRows(rows2)

	sanDir := t.TempDir()
	if err := streamTable(context.Background(), sqlDB, table, sanDir, SanitizeByPattern); err != nil {
		t.Fatal(err)
	}
	sanitized, err := os.ReadFile(filepath.Join(sanDir, "users.ndjson"))
	if err != nil {
		t.Fatal(err)
	}

	plainLine := strings.TrimSpace(string(plain))
	sanLine := strings.TrimSpace(string(sanitized))

	if !strings.Contains(plainLine, "alice@corp.test") {
		t.Fatalf("plain dump should contain original email: %s", plainLine)
	}
	if strings.Contains(sanLine, "alice@corp.test") {
		t.Fatalf("sanitized dump should not contain original email: %s", sanLine)
	}
	if !strings.Contains(sanLine, "redacted@example.com") {
		t.Fatalf("sanitized dump should contain placeholder: %s", sanLine)
	}
	if !strings.Contains(plainLine, `"name":"Alice"`) || !strings.Contains(sanLine, `"name":"Alice"`) {
		t.Fatalf("name column should be unchanged in both dumps")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyRowTransformReceivesMetadata(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
			{Name: "email", DataType: "text"},
		},
	}
	row := map[string]any{"id": 1, "email": "a@b.c"}

	var gotSchema, gotTable string
	var gotColumns []db.Column
	transform := func(schema, name string, columns []db.Column, r map[string]any) (map[string]any, error) {
		gotSchema = schema
		gotTable = name
		gotColumns = append([]db.Column(nil), columns...)
		out := copyRow(r)
		out["email"] = "mutated@example.com"
		return out, nil
	}

	got, err := applyRowTransform(table, transform, row)
	if err != nil {
		t.Fatal(err)
	}
	if gotSchema != "public" || gotTable != "users" {
		t.Fatalf("metadata = %q.%q, want public.users", gotSchema, gotTable)
	}
	if len(gotColumns) != 2 || gotColumns[0].Name != "id" || gotColumns[1].Name != "email" {
		t.Fatalf("columns = %+v", gotColumns)
	}
	if got["email"] != "mutated@example.com" {
		t.Fatalf("email = %v", got["email"])
	}
}

func TestApplyRowTransformPreservesColumnSet(t *testing.T) {
	table := db.Table{
		Schema: "public",
		Name:   "users",
		Columns: []db.Column{
			{Name: "id", DataType: "integer"},
			{Name: "name", DataType: "text"},
			{Name: "email", DataType: "text"},
		},
	}
	row := map[string]any{"id": 1, "name": "Alice", "email": "alice@corp.test"}

	transform := func(_ string, _ string, _ []db.Column, r map[string]any) (map[string]any, error) {
		out := map[string]any{
			"id":          r["id"],
			"email":       "redacted@example.com",
			"extra_field": "dropped",
		}
		return out, nil
	}

	got, err := applyRowTransform(table, transform, row)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"id", "name", "email"}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing column %q in %v", k, got)
		}
	}
	if len(got) != len(wantKeys) {
		t.Fatalf("got keys %v, want exactly %v", got, wantKeys)
	}
	if got["name"] != "Alice" {
		t.Fatalf("name = %v, want preserved from original row", got["name"])
	}
	if got["email"] != "redacted@example.com" {
		t.Fatalf("email = %v", got["email"])
	}
}

func copyRow(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
