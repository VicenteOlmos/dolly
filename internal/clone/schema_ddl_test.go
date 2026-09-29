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

func TestFormatAlterDomainAddConstraint(t *testing.T) {
	t.Parallel()
	def := "CHECK (VALUE > 0)"
	got := formatAlterDomainAddConstraint("app", "positive_int", "positive_int_check", def)
	want := `ALTER DOMAIN "app"."positive_int" ADD CONSTRAINT "positive_int_check" CHECK (VALUE > 0)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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

func TestFormatAlterTableReplicaIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ident, index string
		want         string
		ok           bool
	}{
		{"f", "", `ALTER TABLE "app"."events" REPLICA IDENTITY FULL`, true},
		{"n", "", `ALTER TABLE "app"."events" REPLICA IDENTITY NOTHING`, true},
		{"i", "events_pkey", `ALTER TABLE "app"."events" REPLICA IDENTITY USING INDEX "events_pkey"`, true},
		{"i", "", "", false},
		{"d", "", "", false},
	}
	for _, tt := range tests {
		got, ok := formatAlterTableReplicaIdentity("app", "events", tt.ident, tt.index)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("ident=%q index=%q: got (%q, %v), want (%q, %v)", tt.ident, tt.index, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFormatCreateCollation(t *testing.T) {
	t.Parallel()
	libc, ok := formatCreateCollation("app", "en_us", "c", "", "en_US.UTF-8", "en_US.UTF-8", true)
	wantLibc := `CREATE COLLATION "app"."en_us" (PROVIDER = libc, LC_COLLATE = 'en_US.UTF-8', LC_CTYPE = 'en_US.UTF-8')`
	if !ok || libc != wantLibc {
		t.Fatalf("libc: got (%q, %v), want (%q, true)", libc, ok, wantLibc)
	}
	icu, ok := formatCreateCollation("app", "und", "i", "und", "", "", false)
	wantICU := `CREATE COLLATION "app"."und" (PROVIDER = icu, LOCALE = 'und', DETERMINISTIC = false)`
	if !ok || icu != wantICU {
		t.Fatalf("icu: got (%q, %v), want (%q, true)", icu, ok, wantICU)
	}
	if _, ok := formatCreateCollation("app", "bad", "c", "", "", "en_US.UTF-8", true); ok {
		t.Fatal("expected skip when libc locale empty")
	}
}

func TestFormatAlterColumnCompression(t *testing.T) {
	t.Parallel()
	got, ok := formatAlterColumnCompression("app", "events", "payload", "l")
	want := `ALTER TABLE ONLY "app"."events" ALTER COLUMN "payload" SET COMPRESSION lz4`
	if !ok || got != want {
		t.Fatalf("lz4: got (%q, %v), want (%q, true)", got, ok, want)
	}
	got, ok = formatAlterColumnCompression("app", "events", "payload", "p")
	want = `ALTER TABLE ONLY "app"."events" ALTER COLUMN "payload" SET COMPRESSION pglz`
	if !ok || got != want {
		t.Fatalf("pglz: got (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestFormatAlterColumnStorage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code string
		want string
		ok   bool
	}{
		{"p", `ALTER TABLE ONLY "app"."docs" ALTER COLUMN "body" SET STORAGE PLAIN`, true},
		{"e", `ALTER TABLE ONLY "app"."docs" ALTER COLUMN "body" SET STORAGE EXTERNAL`, true},
		{"x", `ALTER TABLE ONLY "app"."docs" ALTER COLUMN "body" SET STORAGE EXTENDED`, true},
		{"m", `ALTER TABLE ONLY "app"."docs" ALTER COLUMN "body" SET STORAGE MAIN`, true},
		{"z", "", false},
	}
	for _, tt := range tests {
		got, ok := formatAlterColumnStorage("app", "docs", "body", tt.code)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("code=%q: got (%q, %v), want (%q, %v)", tt.code, got, ok, tt.want, tt.ok)
		}
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

func TestCommentTargetFunctionAndIndex(t *testing.T) {
	t.Parallel()
	fn := commentTarget("function", "app", "my_sum(integer)", "")
	wantFn := `FUNCTION "app"."my_sum"(integer)`
	if fn != wantFn {
		t.Fatalf("function target = %q, want %q", fn, wantFn)
	}
	if got := commentTarget("procedure", "app", "my_proc(integer)", ""); got != `PROCEDURE "app"."my_proc"(integer)` {
		t.Fatalf("procedure target = %q", got)
	}
	constraint := commentTarget("constraint", "app", "orders", "orders_total_check")
	wantConstraint := `CONSTRAINT "orders_total_check" ON "app"."orders"`
	if constraint != wantConstraint {
		t.Fatalf("constraint target = %q, want %q", constraint, wantConstraint)
	}
	if got := commentTarget("domain_constraint", "app", "email", "email_check"); got != `CONSTRAINT "email_check" ON DOMAIN "app"."email"` {
		t.Fatalf("domain constraint target = %q", got)
	}
	domain := commentTarget("domain", "app", "email", "")
	wantDomain := `DOMAIN "app"."email"`
	if domain != wantDomain {
		t.Fatalf("domain target = %q, want %q", domain, wantDomain)
	}
	if got := commentTarget("type", "app", "status_enum", ""); got != `TYPE "app"."status_enum"` {
		t.Fatalf("type target = %q", got)
	}
	gotTypeComment := formatCommentOn("type", "app", "status_enum", "", "lifecycle")
	wantTypeComment := `COMMENT ON TYPE "app"."status_enum" IS 'lifecycle'`
	if gotTypeComment != wantTypeComment {
		t.Fatalf("type comment = %q, want %q", gotTypeComment, wantTypeComment)
	}
	idx := commentTarget("index", "app", "users_email_idx", "")
	wantIdx := `INDEX "app"."users_email_idx"`
	if idx != wantIdx {
		t.Fatalf("index target = %q, want %q", idx, wantIdx)
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

func TestFormatGrantColumn(t *testing.T) {
	t.Parallel()
	got := formatGrantColumn("SELECT, UPDATE", "app", "users", "email", "app_reader")
	want := `GRANT SELECT ("email"), UPDATE ("email") ON TABLE "app"."users" TO "app_reader"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatGrantSequenceAndRoutine(t *testing.T) {
	t.Parallel()
	seq := formatGrantSequence("SELECT, USAGE", "app", "users_id_seq", "app_reader")
	wantSeq := `GRANT SELECT, USAGE ON SEQUENCE "app"."users_id_seq" TO "app_reader"`
	if seq != wantSeq {
		t.Fatalf("sequence grant = %q, want %q", seq, wantSeq)
	}
	routine := formatGrantRoutine("app", "my_sum", "integer", "FUNCTION", "app_reader")
	wantRoutine := `GRANT EXECUTE ON FUNCTION "app"."my_sum"(integer) TO "app_reader"`
	if routine != wantRoutine {
		t.Fatalf("routine grant = %q, want %q", routine, wantRoutine)
	}
	if got := formatGrantRoutine("app", "my_proc", "integer", "PROCEDURE", "PUBLIC"); got != `GRANT EXECUTE ON PROCEDURE "app"."my_proc"(integer) TO PUBLIC` {
		t.Fatalf("procedure grant = %q", got)
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
