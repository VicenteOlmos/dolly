package clone

import (
	"strings"
	"testing"
)

func TestFormatCreateEnumType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		schema string
		name   string
		labels []string
		want   string
	}{
		{
			schema: "billing",
			name:   "status_enum",
			labels: []string{"active", "inactive"},
			want:   `CREATE TYPE "billing"."status_enum" AS ENUM ('active', 'inactive')`,
		},
	}
	for _, tt := range tests {
		got := formatCreateEnumType(tt.schema, tt.name, tt.labels)
		if got != tt.want {
			t.Fatalf("formatCreateEnumType() = %q, want %q", got, tt.want)
		}
	}
}

func TestFormatCreateDomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		schema  string
		typ     string
		base    string
		notNull bool
		def     string
		want    string
	}{
		{
			name:   "with default",
			schema: "app",
			typ:    "email",
			base:   "text",
			def:    "lower('')",
			want:   `CREATE DOMAIN "app"."email" AS text DEFAULT lower('')`,
		},
		{
			name:    "not null",
			schema:  "app",
			typ:     "positive_int",
			base:    "integer",
			notNull: true,
			want:    `CREATE DOMAIN "app"."positive_int" AS integer NOT NULL`,
		},
	}
	for _, tt := range tests {
		got := formatCreateDomain(tt.schema, tt.typ, tt.base, tt.notNull, tt.def)
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatCreateSequence(t *testing.T) {
	t.Parallel()
	got := formatCreateSequence("app", "users_id_seq", sequenceDef{
		increment:  1,
		minValue:   1,
		maxValue:   9223372036854775807,
		startValue: 1,
		cache:      1,
		minValid:   true,
		maxValid:   true,
		startValid: true,
	})
	if !strings.Contains(got, `CREATE SEQUENCE "app"."users_id_seq"`) {
		t.Fatalf("missing sequence: %q", got)
	}
	if !strings.Contains(got, "INCREMENT BY 1") || !strings.Contains(got, "START WITH 1") {
		t.Fatalf("missing options: %q", got)
	}
}

func TestFormatAlterSequenceOwnedBy(t *testing.T) {
	t.Parallel()
	want := `ALTER SEQUENCE "app"."users_id_seq" OWNED BY "app"."users"."id"`
	got := formatAlterSequenceOwnedBy("app", "users_id_seq", "app", "users", "id")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatCreateExtension(t *testing.T) {
	t.Parallel()
	got := formatCreateExtension("uuid-ossp")
	want := `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatTableCheckConstraint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		def  string
		want string
	}{
		{
			name: "check_prefix",
			def:  "CHECK (amount > 0)",
			want: `CONSTRAINT "amount_positive" CHECK (amount > 0)`,
		},
		{
			name: "bare_expr",
			def:  "amount > 0",
			want: `CONSTRAINT "amount_positive" CHECK (amount > 0)`,
		},
	}
	for _, tt := range tests {
		got := formatTableCheckConstraint("amount_positive", tt.def)
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestFormatAlterTableAddConstraintForeignKey(t *testing.T) {
	t.Parallel()
	def := `FOREIGN KEY ("user_id") REFERENCES "app"."users" ("id") ON DELETE CASCADE`
	got := formatAlterTableAddConstraint("billing", "accounts", "accounts_user_id_fkey", def)
	if !strings.Contains(got, "ON DELETE CASCADE") {
		t.Fatalf("missing actions: %q", got)
	}
}

func TestFormatCreateView(t *testing.T) {
	t.Parallel()
	got := formatCreateView("app", "active_users", "SELECT id FROM users WHERE active", false)
	if !strings.HasPrefix(got, `CREATE VIEW "app"."active_users" AS `) {
		t.Fatalf("got %q", got)
	}
	gotMat := formatCreateView("app", "mv", "SELECT 1", true)
	if !strings.HasPrefix(gotMat, `CREATE MATERIALIZED VIEW "app"."mv" AS `) {
		t.Fatalf("got %q", gotMat)
	}
}

func TestFormatCommentOn(t *testing.T) {
	t.Parallel()
	got := formatCommentOn("column", "app", "users", "email", "primary contact")
	want := `COMMENT ON COLUMN "app"."users"."email" IS 'primary contact'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGrantTable(t *testing.T) {
	t.Parallel()
	got := formatGrantTable("SELECT, INSERT", "app", "users", "app_reader")
	want := `GRANT SELECT, INSERT ON TABLE "app"."users" TO "app_reader"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatEnableRLSAndPolicy(t *testing.T) {
	t.Parallel()
	rls := formatEnableRLS("app", "users", false)
	if !strings.Contains(rls, "ENABLE ROW LEVEL SECURITY") {
		t.Fatalf("rls stmt: %q", rls)
	}
	pol := formatCreatePolicy("app", "users", policyDef{
		name:       "tenant_isolation",
		command:    "ALL",
		roles:      []string{"app_user"},
		using:      "tenant_id = current_setting('app.tenant_id')::int",
		permissive: true,
	})
	if !strings.Contains(pol, `CREATE POLICY "tenant_isolation"`) || !strings.Contains(pol, "USING (") {
		t.Fatalf("policy stmt: %q", pol)
	}
}

func TestFormatCreateCompositeType(t *testing.T) {
	t.Parallel()
	got := formatCreateCompositeType("app", "address", []compositeAttr{
		{name: "street", typ: "text"},
		{name: "zip", typ: "integer"},
	})
	want := `CREATE TYPE "app"."address" AS ("street" text, "zip" integer)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
