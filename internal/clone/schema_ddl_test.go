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

func TestFormatSequenceDataType(t *testing.T) {
	t.Parallel()
	bigint := formatCreateSequence("app", "users_id_seq", sequenceDef{
		dataType:   "bigint",
		increment:  1,
		minValid:   true,
		maxValid:   true,
		startValid: true,
	})
	if strings.Contains(bigint, " AS ") {
		t.Fatalf("bigint should omit AS: %q", bigint)
	}
	intSeq := formatCreateSequence("app", "small_id_seq", sequenceDef{
		dataType:   "integer",
		increment:  1,
		minValid:   true,
		maxValid:   true,
		startValid: true,
	})
	if !strings.Contains(intSeq, " AS integer") {
		t.Fatalf("integer sequence missing AS: %q", intSeq)
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
	got := formatCreateExtension("uuid-ossp", "public", "")
	want := `CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA "public"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = formatCreateExtension("postgis", "gis", "3.4.0")
	want = `CREATE EXTENSION IF NOT EXISTS "postgis" SCHEMA "gis" VERSION '3.4.0'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = formatCreateExtension("plpgsql", "pg_catalog", "")
	if strings.Contains(got, "SCHEMA") {
		t.Fatalf("pg_catalog schema must stay omitted: %q", got)
	}
}

func TestFormatCommentOnTriggerAndRule(t *testing.T) {
	t.Parallel()
	got := formatCommentOn("trigger", "app", "items", "touch", "keeps updated_at")
	want := `COMMENT ON TRIGGER "touch" ON "app"."items" IS 'keeps updated_at'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = formatCommentOn("rule", "app", "items", "log_del", "audit")
	want = `COMMENT ON RULE "log_del" ON "app"."items" IS 'audit'`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatAlterViewOptions(t *testing.T) {
	t.Parallel()
	got, ok := formatAlterViewOptions("app", "active", false, "security_barrier=true,security_invoker=true,check_option=cascaded,fillfactor=70")
	if !ok {
		t.Fatal("expected options")
	}
	want := `ALTER VIEW "app"."active" SET (security_barrier=true, security_invoker=true, check_option=cascaded)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, ok := formatAlterViewOptions("app", "mv", true, "security_invoker=true"); ok {
		t.Fatal("materialized views do not take security_invoker")
	}
	if _, ok := formatAlterViewOptions("app", "mv", true, "security_barrier=true"); ok {
		t.Fatal("materialized views do not take security_barrier")
	}
	got, ok = formatAlterViewOptions("app", "mv", true, "fillfactor=70")
	if !ok || got != `ALTER MATERIALIZED VIEW "app"."mv" SET (fillfactor=70)` {
		t.Fatalf("matview = %q ok=%v", got, ok)
	}
	if _, ok := formatAlterViewOptions("app", "mv", true, "fillfactor=nope"); ok {
		t.Fatal("non-integer fillfactor must be omitted")
	}
}

func TestFormatCreateRangeType(t *testing.T) {
	t.Parallel()
	got := formatCreateRangeType(rangeTypeDef{
		schema: "app", name: "span", subtype: "integer",
		opclassSchema: "pg_catalog", opclass: "int4_ops",
		canonical:        `"app"."span_canonical"`,
		subtypeDiff:      `"app"."span_diff"`,
		multirangeSchema: "app",
		multirange:       "span_set",
	})
	want := `CREATE TYPE "app"."span" AS RANGE (SUBTYPE = integer, SUBTYPE_OPCLASS = "int4_ops", CANONICAL = "app"."span_canonical", SUBTYPE_DIFF = "app"."span_diff", MULTIRANGE_TYPE_NAME = "app"."span_set")`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	got = formatCreateRangeType(rangeTypeDef{
		schema: "app", name: "span", subtype: "integer",
		opclassSchema: "app", opclass: "custom_ops",
		multirange: "span_multirange",
	})
	if strings.Contains(got, "custom_ops") || strings.Contains(got, "MULTIRANGE") {
		t.Fatalf("custom opclass and default multirange must stay omitted: %s", got)
	}
	if formatCreateRangeShell("app", "span") != `CREATE TYPE "app"."span"` {
		t.Fatal("shell type")
	}
	if !(rangeTypeDef{canonicalSchema: "app"}).needsShell() || (rangeTypeDef{canonicalSchema: "pg_catalog"}).needsShell() {
		t.Fatal("shell is required only for functions outside pg_catalog")
	}
	if defaultMultirangeName("int4range") != "int4multirange" || defaultMultirangeName("span") != "span_multirange" {
		t.Fatal("default multirange name")
	}
}

func TestFormatTableExcludeConstraint(t *testing.T) {
	t.Parallel()
	def := "EXCLUDE USING gist (room WITH =, during WITH &&)"
	got := formatTableExcludeConstraint("bookings_room_during_excl", def)
	want := `CONSTRAINT "bookings_room_during_excl" EXCLUDE USING gist (room WITH =, during WITH &&)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatTablePrimaryKeyConstraint(t *testing.T) {
	t.Parallel()
	got := formatTablePrimaryKeyConstraint(primaryConstraint{
		name: "items_pkey", columns: []string{"id"}, deferrable: true, deferred: true,
	})
	want := `CONSTRAINT "items_pkey" PRIMARY KEY ("id") DEFERRABLE INITIALLY DEFERRED`
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
	libc, ok := formatCreateCollation("app", "en_us", "c", "", "", "en_US.UTF-8", "en_US.UTF-8", true)
	wantLibc := `CREATE COLLATION "app"."en_us" (PROVIDER = libc, LC_COLLATE = 'en_US.UTF-8', LC_CTYPE = 'en_US.UTF-8')`
	if !ok || libc != wantLibc {
		t.Fatalf("libc: got (%q, %v), want (%q, true)", libc, ok, wantLibc)
	}
	icu, ok := formatCreateCollation("app", "und", "i", "und", "", "", "", false)
	wantICU := `CREATE COLLATION "app"."und" (PROVIDER = icu, LOCALE = 'und', DETERMINISTIC = false)`
	if !ok || icu != wantICU {
		t.Fatalf("icu: got (%q, %v), want (%q, true)", icu, ok, wantICU)
	}
	icu, ok = formatCreateCollation("app", "custom", "i", "und", "&V << w <<< W's", "", "", true)
	wantICU = `CREATE COLLATION "app"."custom" (PROVIDER = icu, LOCALE = 'und', DETERMINISTIC = true, RULES = '&V << w <<< W''s')`
	if !ok || icu != wantICU {
		t.Fatalf("icu rules: got (%q, %v), want (%q, true)", icu, ok, wantICU)
	}
	if _, ok := formatCreateCollation("app", "bad", "c", "", "", "", "en_US.UTF-8", true); ok {
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

func TestFormatAlterTableFillfactor(t *testing.T) {
	t.Parallel()
	got := formatAlterTableFillfactor("app", "events", 90)
	want := `ALTER TABLE "app"."events" SET (fillfactor=90)`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatAlterTableReloptions(t *testing.T) {
	t.Parallel()
	got, ok := formatAlterTableReloptions("app", "events", map[string]string{
		"parallel_workers":                   "4",
		"autovacuum_enabled":                 "false",
		"autovacuum_vacuum_scale_factor":     "0.15",
		"autovacuum_analyze_scale_factor":    "0.05",
		"toast_tuple_target":                 "2048",
		"fillfactor":                         "90",
		"unknown_option":                     "nope",
	})
	want := `ALTER TABLE "app"."events" SET (autovacuum_analyze_scale_factor=0.05, autovacuum_enabled=off, autovacuum_vacuum_scale_factor=0.15, fillfactor=90, parallel_workers=4, toast_tuple_target=2048)`
	if !ok || got != want {
		t.Fatalf("got (%q, %v), want (%q, true)", got, ok, want)
	}
	if _, ok := formatAlterTableReloptions("app", "events", map[string]string{"fillfactor": "nope"}); ok {
		t.Fatal("invalid fillfactor must be omitted")
	}
}

func TestFormatTableReloptionFragmentAutovacuumEnabledOff(t *testing.T) {
	t.Parallel()
	fragment, ok := formatTableReloptionFragment("autovacuum_enabled", "off")
	if !ok || fragment != "autovacuum_enabled=off" {
		t.Fatalf("got (%q, %v), want (autovacuum_enabled=off, true)", fragment, ok)
	}
	got, ok := formatAlterTableReloptions("app", "events", map[string]string{"autovacuum_enabled": "off"})
	want := `ALTER TABLE "app"."events" SET (autovacuum_enabled=off)`
	if !ok || got != want {
		t.Fatalf("got (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestFormatAlterColumnStatistics(t *testing.T) {
	t.Parallel()
	got := formatAlterColumnStatistics("app", "events", "payload", 1000)
	want := `ALTER TABLE ONLY "app"."events" ALTER COLUMN "payload" SET STATISTICS 1000`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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
	got := formatCreateView("app", "active_users", "SELECT id FROM users WHERE active", false, true)
	if !strings.HasPrefix(got, `CREATE VIEW "app"."active_users" AS `) {
		t.Fatalf("got %q", got)
	}
	gotMat := formatCreateView("app", "mv", "SELECT 1", true, true)
	gotEmpty := formatCreateView("app", "mv_empty", "SELECT 1", true, false)
	if !strings.Contains(gotEmpty, "WITH NO DATA") {
		t.Fatalf("unpopulated matview = %s", gotEmpty)
	}
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
	if got := commentTarget("collation", "app", "custom", ""); got != `COLLATION "app"."custom"` {
		t.Fatalf("collation target = %q", got)
	}
	gotCollationComment := formatCommentOn("collation", "app", "custom", "", "sort rules")
	wantCollationComment := `COMMENT ON COLLATION "app"."custom" IS 'sort rules'`
	if gotCollationComment != wantCollationComment {
		t.Fatalf("collation comment = %q, want %q", gotCollationComment, wantCollationComment)
	}
	if got := commentTarget("policy", "app", "users", "tenant_isolation"); got != `POLICY "tenant_isolation" ON "app"."users"` {
		t.Fatalf("policy target = %q", got)
	}
	gotPolicyComment := formatCommentOn("policy", "app", "users", "tenant_isolation", "tenant filter")
	wantPolicyComment := `COMMENT ON POLICY "tenant_isolation" ON "app"."users" IS 'tenant filter'`
	if gotPolicyComment != wantPolicyComment {
		t.Fatalf("policy comment = %q, want %q", gotPolicyComment, wantPolicyComment)
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

func TestFormatCreateCast(t *testing.T) {
	t.Parallel()
	withFn, ok := formatCreateCast(castRow{
		srcSchema: "app", srcType: "status_enum",
		tgtSchema: "app", tgtType: "text",
		castMethod: "f", castContext: "a",
		fnSchema: "app", fnName: "status_to_text", fnArgs: "app.status_enum",
	})
	if !ok {
		t.Fatal("expected cast with function")
	}
	wantWithFn := `CREATE CAST ("app"."status_enum" AS "app"."text") WITH FUNCTION "app"."status_to_text"(app.status_enum) AS ASSIGNMENT`
	if withFn != wantWithFn {
		t.Fatalf("got %q, want %q", withFn, wantWithFn)
	}
	implicit, ok := formatCreateCast(castRow{
		srcSchema: "app", srcType: "small_id",
		tgtSchema: "app", tgtType: "bigint",
		castMethod: "b", castContext: "e",
	})
	if !ok {
		t.Fatal("expected binary cast")
	}
	wantImplicit := `CREATE CAST ("app"."small_id" AS "app"."bigint") WITHOUT FUNCTION AS IMPLICIT`
	if implicit != wantImplicit {
		t.Fatalf("got %q, want %q", implicit, wantImplicit)
	}
	if _, ok := formatCreateCast(castRow{
		srcSchema: "app", srcType: "a",
		tgtSchema: "app", tgtType: "b",
		castMethod: "f", castContext: "i",
	}); ok {
		t.Fatal("expected missing function cast to be skipped")
	}
	inout, ok := formatCreateCast(castRow{
		srcSchema: "app", srcType: "in_src",
		tgtSchema: "app", tgtType: "in_dst",
		castMethod: "i", castContext: "e",
	})
	if !ok {
		t.Fatal("expected inout cast")
	}
	wantInout := `CREATE CAST ("app"."in_src" AS "app"."in_dst") WITH INOUT AS IMPLICIT`
	if inout != wantInout {
		t.Fatalf("inout cast: got %q, want %q", inout, wantInout)
	}
}

func TestCommentTargetAggregateAndStatistics(t *testing.T) {
	t.Parallel()
	if got := commentTarget("aggregate", "app", "my_avg", "integer"); got != `AGGREGATE "app"."my_avg"(integer)` {
		t.Fatalf("aggregate target = %q", got)
	}
	if got := commentTarget("aggregate", "app", `a(b`, "integer"); got != `AGGREGATE "app"."a(b"(integer)` {
		t.Fatalf("aggregate name with parenthesis = %q", got)
	}
	if got := formatCommentOn("aggregate", "app", `a(b`, "integer", "note"); got != `COMMENT ON AGGREGATE "app"."a(b"(integer) IS 'note'` {
		t.Fatalf("aggregate comment = %q", got)
	}
	if got := formatCommentOn("statistics", "app", "users_stats", "", "correlation hints"); got != `COMMENT ON STATISTICS "app"."users_stats" IS 'correlation hints'` {
		t.Fatalf("statistics comment = %q", got)
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
	gotType := formatGrantType("app", "status_enum", "app_reader")
	wantType := `GRANT USAGE ON TYPE "app"."status_enum" TO "app_reader"`
	if gotType != wantType {
		t.Fatalf("type grant = %q, want %q", gotType, wantType)
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
	gotColl := formatCreateCompositeType("app", "label", []compositeAttr{
		{name: "text", typ: "text", collSchema: "app", collName: "custom"},
	})
	wantColl := `CREATE TYPE "app"."label" AS ("text" text COLLATE "app"."custom")`
	if gotColl != wantColl {
		t.Fatalf("collated composite: got %q, want %q", gotColl, wantColl)
	}
}
