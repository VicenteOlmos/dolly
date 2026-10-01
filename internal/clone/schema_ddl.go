package clone

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// formatCreateEnumType emits CREATE TYPE ... AS ENUM (...).
func formatCreateEnumType(schema, name string, labels []string) string {
	quoted := make([]string, len(labels))
	for i, label := range labels {
		quoted[i] = quoteLiteral(label)
	}
	return fmt.Sprintf(
		"CREATE TYPE %s AS ENUM (%s)",
		quoteQualifiedType(schema, name),
		strings.Join(quoted, ", "),
	)
}

// formatCreateDomain emits CREATE DOMAIN.
func formatCreateDomain(schema, name, baseType string, notNull bool, defaultExpr string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("CREATE DOMAIN %s AS %s", quoteQualifiedType(schema, name), baseType))
	if defaultExpr != "" {
		parts = append(parts, "DEFAULT "+defaultExpr)
	}
	if notNull {
		parts = append(parts, "NOT NULL")
	}
	return strings.Join(parts, " ")
}

// formatCreateCompositeType emits CREATE TYPE ... AS (...).
func formatCreateCompositeType(schema, name string, attrs []compositeAttr) string {
	var fieldParts []string
	for _, a := range attrs {
		part := fmt.Sprintf("%s %s", quoteIdentifier(a.name), a.typ)
		if a.collSchema != "" && a.collName != "" {
			part += " COLLATE " + quoteQualifiedType(a.collSchema, a.collName)
		}
		fieldParts = append(fieldParts, part)
	}
	return fmt.Sprintf(
		"CREATE TYPE %s AS (%s)",
		quoteQualifiedType(schema, name),
		strings.Join(fieldParts, ", "),
	)
}

type compositeAttr struct {
	name       string
	typ        string
	collSchema string
	collName   string
}

// formatCreateSequence emits CREATE SEQUENCE with catalog-derived options.
func formatCreateSequence(schema, name string, seq sequenceDef) string {
	return fmt.Sprintf("CREATE SEQUENCE %s%s", quoteQualifiedTable(schema, name), formatSequenceOptions(seq))
}

func formatSequenceOptions(seq sequenceDef) string {
	var stmt string
	if dt := strings.TrimSpace(seq.dataType); dt != "" && !strings.EqualFold(dt, "bigint") {
		stmt += " AS " + dt
	}
	if seq.increment != 0 {
		stmt += fmt.Sprintf(" INCREMENT BY %d", seq.increment)
	}
	if seq.minValue != 0 || seq.minValid {
		stmt += fmt.Sprintf(" MINVALUE %d", seq.minValue)
	}
	if seq.maxValue != 0 || seq.maxValid {
		stmt += fmt.Sprintf(" MAXVALUE %d", seq.maxValue)
	}
	if seq.startValue != 0 || seq.startValid {
		stmt += fmt.Sprintf(" START WITH %d", seq.startValue)
	}
	if seq.cache != 0 {
		stmt += fmt.Sprintf(" CACHE %d", seq.cache)
	}
	if seq.cycle {
		stmt += " CYCLE"
	}
	return stmt
}

type sequenceDef struct {
	dataType   string
	increment  int64
	minValue   int64
	maxValue   int64
	startValue int64
	cache      int64
	cycle      bool
	minValid   bool
	maxValid   bool
	startValid bool
}

// formatAlterSequenceOwnedBy emits ALTER SEQUENCE ... OWNED BY.
func formatAlterSequenceOwnedBy(schema, seqName, tableSchema, tableName, column string) string {
	return fmt.Sprintf(
		"ALTER SEQUENCE %s OWNED BY %s.%s",
		quoteQualifiedTable(schema, seqName),
		quoteQualifiedTable(tableSchema, tableName),
		quoteIdentifier(column),
	)
}

// formatCreateExtension emits CREATE EXTENSION IF NOT EXISTS.
// The source schema is always named, including public, so the target
// search_path cannot install the extension somewhere else. pg_catalog is
// omitted. An explicit version is replayed so the target does not install
// the extension at its default version.
func formatCreateExtension(name, schema, version string) string {
	stmt := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", quoteIdentifier(name))
	schema = strings.TrimSpace(schema)
	if schema != "" && !strings.EqualFold(schema, "pg_catalog") {
		stmt += " SCHEMA " + quoteIdentifier(schema)
	}
	if version = strings.TrimSpace(version); version != "" {
		stmt += " VERSION " + quoteLiteral(version)
	}
	return stmt
}

// formatTableCheckConstraint adds CONSTRAINT name CHECK (...).
func formatTableCheckConstraint(name, pgConstraintDef string) string {
	def := strings.TrimSpace(pgConstraintDef)
	if strings.HasPrefix(strings.ToUpper(def), "CHECK") {
		return fmt.Sprintf("CONSTRAINT %s %s", quoteIdentifier(name), def)
	}
	return fmt.Sprintf("CONSTRAINT %s CHECK (%s)", quoteIdentifier(name), def)
}

// formatTableExcludeConstraint adds CONSTRAINT name EXCLUDE (...).
func formatTableExcludeConstraint(name, pgConstraintDef string) string {
	def := strings.TrimSpace(pgConstraintDef)
	if strings.HasPrefix(strings.ToUpper(def), "EXCLUDE") {
		return fmt.Sprintf("CONSTRAINT %s %s", quoteIdentifier(name), def)
	}
	return fmt.Sprintf("CONSTRAINT %s EXCLUDE %s", quoteIdentifier(name), def)
}

// formatTablePrimaryKeyConstraint adds CONSTRAINT name PRIMARY KEY (...).
func formatTablePrimaryKeyConstraint(pk primaryConstraint) string {
	quoted := make([]string, len(pk.columns))
	for i, name := range pk.columns {
		quoted[i] = quoteIdentifier(name)
	}
	clause := fmt.Sprintf("CONSTRAINT %s PRIMARY KEY (%s)", quoteIdentifier(pk.name), strings.Join(quoted, ", "))
	if pk.deferrable {
		if pk.deferred {
			clause += " DEFERRABLE INITIALLY DEFERRED"
		} else {
			clause += " DEFERRABLE"
		}
	}
	return clause
}

// formatAlterDomainAddConstraint uses pg_get_constraintdef output for domain CHECK.
func formatAlterDomainAddConstraint(schema, domain, constraintName, pgConstraintDef string) string {
	def := strings.TrimSpace(pgConstraintDef)
	return fmt.Sprintf(
		"ALTER DOMAIN %s ADD CONSTRAINT %s %s",
		quoteQualifiedType(schema, domain),
		quoteIdentifier(constraintName),
		def,
	)
}

// formatAlterTableAddConstraint uses pg_get_constraintdef output for FOREIGN KEY.
func formatAlterTableAddConstraint(schema, table, constraintName, pgConstraintDef string) string {
	def := strings.TrimSpace(pgConstraintDef)
	return fmt.Sprintf(
		"ALTER TABLE %s ADD CONSTRAINT %s %s",
		quoteQualifiedTable(schema, table),
		quoteIdentifier(constraintName),
		def,
	)
}

// formatAlterTableReplicaIdentity emits ALTER TABLE ... REPLICA IDENTITY.
func formatAlterTableReplicaIdentity(schema, table, ident, indexName string) (string, bool) {
	qual := quoteQualifiedTable(schema, table)
	switch ident {
	case "f":
		return fmt.Sprintf("ALTER TABLE %s REPLICA IDENTITY FULL", qual), true
	case "n":
		return fmt.Sprintf("ALTER TABLE %s REPLICA IDENTITY NOTHING", qual), true
	case "i":
		indexName = strings.TrimSpace(indexName)
		if indexName == "" {
			return "", false
		}
		return fmt.Sprintf(
			"ALTER TABLE %s REPLICA IDENTITY USING INDEX %s",
			qual,
			quoteIdentifier(indexName),
		), true
	default:
		return "", false
	}
}

// formatCreateCollation emits CREATE COLLATION for libc or ICU user collations.
func formatCreateCollation(schema, name, provider, icuLocale, icuRules, libcCollate, libcCtype string, deterministic bool) (string, bool) {
	qual := quoteQualifiedType(schema, name)
	switch provider {
	case "c":
		if libcCollate == "" || libcCtype == "" {
			return "", false
		}
		return fmt.Sprintf(
			"CREATE COLLATION %s (PROVIDER = libc, LC_COLLATE = %s, LC_CTYPE = %s)",
			qual,
			quoteLiteral(libcCollate),
			quoteLiteral(libcCtype),
		), true
	case "i":
		if icuLocale == "" {
			return "", false
		}
		det := "true"
		if !deterministic {
			det = "false"
		}
		rules := ""
		if icuRules != "" {
			rules = ", RULES = " + quoteLiteral(icuRules)
		}
		return fmt.Sprintf(
			"CREATE COLLATION %s (PROVIDER = icu, LOCALE = %s, DETERMINISTIC = %s%s)",
			qual,
			quoteLiteral(icuLocale),
			det,
			rules,
		), true
	default:
		return "", false
	}
}

// formatAlterColumnCompression emits ALTER TABLE ... SET COMPRESSION.
func formatAlterColumnCompression(schema, table, column, code string) (string, bool) {
	var compression string
	switch code {
	case "l":
		compression = "lz4"
	case "p":
		compression = "pglz"
	default:
		return "", false
	}
	return fmt.Sprintf(
		"ALTER TABLE ONLY %s ALTER COLUMN %s SET COMPRESSION %s",
		quoteQualifiedTable(schema, table),
		quoteIdentifier(column),
		compression,
	), true
}

// formatAlterTableFillfactor emits ALTER TABLE ... SET (fillfactor=N).
func formatAlterTableFillfactor(schema, table string, fillfactor int) string {
	stmt, _ := formatAlterTableReloptions(schema, table, map[string]string{
		"fillfactor": strconv.Itoa(fillfactor),
	})
	return stmt
}

// formatTableReloptionFragment returns a single reloption assignment for ALTER TABLE SET.
func formatTableReloptionFragment(name, rawValue string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	rawValue = strings.TrimSpace(rawValue)
	switch name {
	case "fillfactor":
		ff, err := parseFillfactorOption(rawValue)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("fillfactor=%d", ff), true
	case "autovacuum_enabled":
		emit, ok := formatReloptionBooleanValue(rawValue)
		if !ok {
			return "", false
		}
		return "autovacuum_enabled=" + emit, true
	case "autovacuum_vacuum_scale_factor", "autovacuum_analyze_scale_factor":
		if !isReloptionNumericText(rawValue) {
			return "", false
		}
		return name + "=" + rawValue, true
	case "toast_tuple_target", "parallel_workers":
		n, err := strconv.Atoi(rawValue)
		if err != nil || n < 0 {
			return "", false
		}
		return fmt.Sprintf("%s=%d", name, n), true
	default:
		return "", false
	}
}

// formatReloptionBooleanValue normalizes PostgreSQL boolean reloption spellings for replay.
// Accepted (case-insensitive): on/off/true/false/yes/no/1/0. Emits on/off, or the stored
// on/off token when that is what PostgreSQL recorded.
func formatReloptionBooleanValue(rawValue string) (string, bool) {
	trimmed := strings.TrimSpace(rawValue)
	lower := strings.ToLower(trimmed)
	switch lower {
	case "on", "true", "yes", "1":
		if lower == "on" || lower == "off" {
			return lower, true
		}
		return "on", true
	case "off", "false", "no", "0":
		if lower == "on" || lower == "off" {
			return lower, true
		}
		return "off", true
	default:
		return "", false
	}
}

func isReloptionNumericText(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// formatAlterTableReloptions emits ALTER TABLE ... SET (...) for supported table reloptions.
func formatAlterTableReloptions(schema, table string, options map[string]string) (string, bool) {
	names := make([]string, 0, len(options))
	for name := range options {
		names = append(names, name)
	}
	sort.Strings(names)

	var parts []string
	for _, name := range names {
		fragment, ok := formatTableReloptionFragment(name, options[name])
		if !ok {
			continue
		}
		parts = append(parts, fragment)
	}
	if len(parts) == 0 {
		return "", false
	}
	return fmt.Sprintf(
		"ALTER TABLE %s SET (%s)",
		quoteQualifiedTable(schema, table),
		strings.Join(parts, ", "),
	), true
}

// formatAlterColumnStatistics emits ALTER TABLE ... ALTER COLUMN ... SET STATISTICS.
func formatAlterColumnStatistics(schema, table, column string, target int) string {
	return fmt.Sprintf(
		"ALTER TABLE ONLY %s ALTER COLUMN %s SET STATISTICS %d",
		quoteQualifiedTable(schema, table),
		quoteIdentifier(column),
		target,
	)
}

// formatAlterColumnStorage emits ALTER TABLE ... ALTER COLUMN ... SET STORAGE.
func formatAlterColumnStorage(schema, table, column, storageCode string) (string, bool) {
	var storage string
	switch storageCode {
	case "p":
		storage = "PLAIN"
	case "e":
		storage = "EXTERNAL"
	case "x":
		storage = "EXTENDED"
	case "m":
		storage = "MAIN"
	default:
		return "", false
	}
	return fmt.Sprintf(
		"ALTER TABLE ONLY %s ALTER COLUMN %s SET STORAGE %s",
		quoteQualifiedTable(schema, table),
		quoteIdentifier(column),
		storage,
	), true
}

// formatCreateView emits CREATE [MATERIALIZED] VIEW. Unpopulated materialized
// views are created WITH NO DATA so replay matches relispopulated on the source.
func formatCreateView(schema, name, definition string, materialized, populated bool) string {
	kind := "VIEW"
	if materialized {
		kind = "MATERIALIZED VIEW"
	}
	def := strings.TrimSpace(definition)
	if strings.HasSuffix(def, ";") {
		def = strings.TrimSuffix(def, ";")
	}
	stmt := fmt.Sprintf(
		"CREATE %s %s AS %s",
		kind,
		quoteQualifiedTable(schema, name),
		def,
	)
	if materialized && !populated {
		stmt += " WITH NO DATA"
	}
	return stmt
}

// formatAlterViewOptions emits ALTER VIEW/MATERIALIZED VIEW SET for reloptions
// that pg_get_viewdef does not include. False and unknown options are omitted.
func formatAlterViewOptions(schema, name string, materialized bool, reloptions string) (string, bool) {
	raw := strings.Trim(strings.TrimSpace(reloptions), "{}")
	if raw == "" {
		return "", false
	}
	var parts []string
	for _, item := range strings.Split(raw, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(item), "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "fillfactor":
			if !materialized {
				continue
			}
			n, err := strconv.Atoi(val)
			if err != nil || n < 10 || n > 100 {
				continue
			}
			parts = append(parts, fmt.Sprintf("fillfactor=%d", n))
		case "security_barrier":
			if materialized || val != "true" {
				continue
			}
			parts = append(parts, "security_barrier=true")
		case "security_invoker", "check_option":
			if materialized {
				continue
			}
			if key == "security_invoker" && val == "true" {
				parts = append(parts, "security_invoker=true")
			}
			if key == "check_option" && (val == "local" || val == "cascaded") {
				parts = append(parts, "check_option="+val)
			}
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	kind := "VIEW"
	if materialized {
		kind = "MATERIALIZED VIEW"
	}
	return fmt.Sprintf("ALTER %s %s SET (%s)", kind, quoteQualifiedTable(schema, name), strings.Join(parts, ", ")), true
}

type rangeTypeDef struct {
	schema            string
	name              string
	subtype           string
	opclassSchema     string
	opclass           string
	collSchema        string
	collName          string
	canonicalSchema   string
	canonical         string
	subtypeDiffSchema string
	subtypeDiff       string
	multirangeSchema  string
	multirange        string
}

func (r rangeTypeDef) needsShell() bool {
	return userRangeFunc(r.canonicalSchema) || userRangeFunc(r.subtypeDiffSchema)
}

func userRangeFunc(schema string) bool {
	schema = strings.TrimSpace(schema)
	return schema != "" && !strings.EqualFold(schema, "pg_catalog")
}

// formatCreateRangeType emits CREATE TYPE ... AS RANGE. Operator classes outside
// pg_catalog are omitted; this does not create operator classes.
func formatCreateRangeType(r rangeTypeDef) string {
	parts := []string{"SUBTYPE = " + strings.TrimSpace(r.subtype)}
	if r.opclassSchema == "pg_catalog" && strings.TrimSpace(r.opclass) != "" {
		parts = append(parts, "SUBTYPE_OPCLASS = "+quoteIdentifier(r.opclass))
	}
	if r.collSchema != "" && r.collName != "" {
		parts = append(parts, "COLLATION = "+quoteQualifiedType(r.collSchema, r.collName))
	}
	if r.canonical != "" {
		parts = append(parts, "CANONICAL = "+r.canonical)
	}
	if r.subtypeDiff != "" {
		parts = append(parts, "SUBTYPE_DIFF = "+r.subtypeDiff)
	}
	if clause, ok := formatMultirangeTypeName(r); ok {
		parts = append(parts, clause)
	}
	return fmt.Sprintf("CREATE TYPE %s AS RANGE (%s)", quoteQualifiedType(r.schema, r.name), strings.Join(parts, ", "))
}

func formatCreateRangeShell(schema, name string) string {
	return "CREATE TYPE " + quoteQualifiedType(schema, name)
}

// formatMultirangeTypeName emits a schema-qualified MULTIRANGE_TYPE_NAME when
// the multirange name or schema differs from PostgreSQL's default. An
// unqualified name is created in search_path, not next to the range type.
func formatMultirangeTypeName(r rangeTypeDef) (string, bool) {
	multi := strings.TrimSpace(r.multirange)
	if multi == "" {
		return "", false
	}
	schema := strings.TrimSpace(r.multirangeSchema)
	if schema == "" {
		schema = r.schema
	}
	customName := multi != defaultMultirangeName(r.name)
	customSchema := !strings.EqualFold(schema, r.schema)
	if !customName && !customSchema {
		return "", false
	}
	return "MULTIRANGE_TYPE_NAME = " + quoteQualifiedType(schema, multi), true
}

// defaultMultirangeName matches PostgreSQL: replace the first "range"
// substring, otherwise append _multirange.
func defaultMultirangeName(rangeName string) string {
	if i := strings.Index(rangeName, "range"); i >= 0 {
		return rangeName[:i] + "multirange" + rangeName[i+len("range"):]
	}
	return rangeName + "_multirange"
}

func formatRangeFunc(schema, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return quoteIdentifier(name)
	}
	return quoteQualifiedType(schema, name)
}

// formatCreateCast emits CREATE CAST for user-defined source and target types.
func formatCreateCast(row castRow) (string, bool) {
	src := quoteQualifiedType(row.srcSchema, row.srcType)
	dst := quoteQualifiedType(row.tgtSchema, row.tgtType)
	stmt := fmt.Sprintf("CREATE CAST (%s AS %s)", src, dst)
	switch row.castMethod {
	case "f":
		if strings.TrimSpace(row.fnName) == "" {
			return "", false
		}
		fn := formatCastFunction(row.fnSchema, row.fnName, row.fnArgs)
		stmt += " WITH FUNCTION " + fn
	case "i":
		stmt += " WITH INOUT"
	case "b":
		stmt += " WITHOUT FUNCTION"
	default:
		return "", false
	}
	switch row.castContext {
	case "e":
		stmt += " AS IMPLICIT"
	case "a":
		stmt += " AS ASSIGNMENT"
	}
	return stmt, true
}

func formatCastFunction(schema, name, args string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	schema = strings.TrimSpace(schema)
	qual := quoteIdentifier(name)
	if schema != "" && !strings.EqualFold(schema, "pg_catalog") {
		qual = quoteQualifiedType(schema, name)
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return qual + "()"
	}
	return qual + "(" + args + ")"
}

// formatCommentOn emits COMMENT ON statements.
func formatCommentOn(kind, schema, object, column, description string) string {
	target := commentTarget(kind, schema, object, column)
	return fmt.Sprintf("COMMENT ON %s IS %s", target, quoteLiteral(description))
}

func commentTarget(kind, schema, object, column string) string {
	switch kind {
	case "schema":
		return "SCHEMA " + quoteIdentifier(schema)
	case "table":
		return "TABLE " + quoteQualifiedTable(schema, object)
	case "column":
		return "COLUMN " + quoteQualifiedTable(schema, object) + "." + quoteIdentifier(column)
	case "view":
		return "VIEW " + quoteQualifiedTable(schema, object)
	case "matview":
		return "MATERIALIZED VIEW " + quoteQualifiedTable(schema, object)
	case "sequence":
		return "SEQUENCE " + quoteQualifiedTable(schema, object)
	case "function", "procedure":
		target := strings.ToUpper(kind)
		fn, argList, ok := strings.Cut(object, "(")
		if !ok || !strings.HasSuffix(argList, ")") {
			return target + " " + quoteQualifiedTable(schema, object)
		}
		argList = strings.TrimSuffix(argList, ")")
		return target + " " + quoteQualifiedType(schema, fn) + "(" + argList + ")"
	case "index":
		return "INDEX " + quoteQualifiedTable(schema, object)
	case "constraint":
		return "CONSTRAINT " + quoteIdentifier(column) + " ON " + quoteQualifiedTable(schema, object)
	case "domain_constraint":
		return "CONSTRAINT " + quoteIdentifier(column) + " ON DOMAIN " + quoteQualifiedType(schema, object)
	case "domain":
		return "DOMAIN " + quoteQualifiedType(schema, object)
	case "type":
		return "TYPE " + quoteQualifiedType(schema, object)
	case "aggregate":
		target := "AGGREGATE "
		args := strings.TrimSpace(column)
		if args == "" {
			return target + quoteQualifiedType(schema, object)
		}
		return target + quoteQualifiedType(schema, object) + "(" + args + ")"
	case "statistics":
		return "STATISTICS " + quoteQualifiedType(schema, object)
	case "collation":
		return "COLLATION " + quoteQualifiedType(schema, object)
	case "policy":
		return "POLICY " + quoteIdentifier(column) + " ON " + quoteQualifiedTable(schema, object)
	case "trigger":
		return "TRIGGER " + quoteIdentifier(column) + " ON " + quoteQualifiedTable(schema, object)
	case "rule":
		return "RULE " + quoteIdentifier(column) + " ON " + quoteQualifiedTable(schema, object)
	default:
		return "TABLE " + quoteQualifiedTable(schema, object)
	}
}

// formatGrantTable emits GRANT privileges ON TABLE.
func formatGrantTable(privileges, schema, table, grantee string) string {
	return fmt.Sprintf(
		"GRANT %s ON TABLE %s TO %s",
		privileges,
		quoteQualifiedTable(schema, table),
		quoteGrantee(grantee),
	)
}

// formatGrantColumn emits GRANT privileges (column) ON TABLE.
func formatGrantColumn(privileges, schema, table, column, grantee string) string {
	parts := strings.Split(privileges, ", ")
	for i, priv := range parts {
		parts[i] = priv + " (" + quoteIdentifier(column) + ")"
	}
	return fmt.Sprintf(
		"GRANT %s ON TABLE %s TO %s",
		strings.Join(parts, ", "),
		quoteQualifiedTable(schema, table),
		quoteGrantee(grantee),
	)
}

// formatGrantSequence emits GRANT privileges ON SEQUENCE.
func formatGrantSequence(privileges, schema, sequence, grantee string) string {
	return fmt.Sprintf(
		"GRANT %s ON SEQUENCE %s TO %s",
		privileges,
		quoteQualifiedTable(schema, sequence),
		quoteGrantee(grantee),
	)
}

// formatGrantRoutine emits GRANT EXECUTE ON FUNCTION or PROCEDURE.
func formatGrantRoutine(schema, name, identityArgs, kind, grantee string) string {
	return fmt.Sprintf(
		"GRANT EXECUTE ON %s %s(%s) TO %s",
		kind,
		quoteQualifiedType(schema, name),
		identityArgs,
		quoteGrantee(grantee),
	)
}

// formatGrantType emits GRANT USAGE ON TYPE.
func formatGrantType(schema, name, grantee string) string {
	return fmt.Sprintf(
		"GRANT USAGE ON TYPE %s TO %s",
		quoteQualifiedType(schema, name),
		quoteGrantee(grantee),
	)
}

// formatGrantSchema emits GRANT privileges ON SCHEMA.
func formatGrantSchema(privileges, schema, grantee string) string {
	return fmt.Sprintf(
		"GRANT %s ON SCHEMA %s TO %s",
		privileges,
		quoteIdentifier(schema),
		quoteGrantee(grantee),
	)
}

// formatAlterDefaultPrivilege emits ALTER DEFAULT PRIVILEGES ... GRANT/REVOKE ... ON objKind.
// When schema is empty the statement is global (no IN SCHEMA).
func formatAlterDefaultPrivilege(ownerRole, schema, objKind, privilege, grantee string, revoke bool) string {
	action := "GRANT"
	dir := "TO"
	if revoke {
		action = "REVOKE"
		dir = "FROM"
	}
	scope := ""
	if schema != "" {
		scope = " IN SCHEMA " + quoteIdentifier(schema)
	}
	return fmt.Sprintf(
		"ALTER DEFAULT PRIVILEGES FOR ROLE %s%s %s %s ON %s %s %s",
		quoteIdentifier(ownerRole),
		scope,
		action,
		privilege,
		objKind,
		dir,
		quoteGrantee(grantee),
	)
}

func quoteGrantee(name string) string {
	if strings.EqualFold(name, "PUBLIC") {
		return "PUBLIC"
	}
	return quoteIdentifier(name)
}

// formatEnableRLS emits ALTER TABLE ... ENABLE ROW LEVEL SECURITY.
func formatEnableRLS(schema, table string, force bool) string {
	_ = force
	return fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", quoteQualifiedTable(schema, table))
}

// formatCreatePolicy emits CREATE POLICY from catalog fields.
func formatCreatePolicy(schema, table string, pol policyDef) string {
	var parts []string
	parts = append(parts, "CREATE POLICY", quoteIdentifier(pol.name), "ON", quoteQualifiedTable(schema, table))
	if pol.permissive {
		parts = append(parts, "AS PERMISSIVE")
	} else {
		parts = append(parts, "AS RESTRICTIVE")
	}
	if pol.command != "" && pol.command != "*" {
		parts = append(parts, "FOR", pol.command)
	}
	if len(pol.roles) > 0 {
		roleParts := make([]string, len(pol.roles))
		for i, r := range pol.roles {
			roleParts[i] = quoteGrantee(r)
		}
		parts = append(parts, "TO", strings.Join(roleParts, ", "))
	}
	if pol.using != "" {
		parts = append(parts, "USING ("+pol.using+")")
	}
	if pol.withCheck != "" {
		parts = append(parts, "WITH CHECK ("+pol.withCheck+")")
	}
	return strings.Join(parts, " ")
}

type policyDef struct {
	name       string
	command    string
	roles      []string
	using      string
	withCheck  string
	permissive bool
}
