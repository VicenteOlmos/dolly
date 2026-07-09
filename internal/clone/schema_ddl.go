package clone

import (
	"fmt"
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
		fieldParts = append(fieldParts, fmt.Sprintf("%s %s", quoteIdentifier(a.name), a.typ))
	}
	return fmt.Sprintf(
		"CREATE TYPE %s AS (%s)",
		quoteQualifiedType(schema, name),
		strings.Join(fieldParts, ", "),
	)
}

type compositeAttr struct {
	name string
	typ  string
}

// formatCreateSequence emits CREATE SEQUENCE with catalog-derived options.
func formatCreateSequence(schema, name string, seq sequenceDef) string {
	stmt := fmt.Sprintf("CREATE SEQUENCE %s", quoteQualifiedTable(schema, name))
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
func formatCreateExtension(name string) string {
	return fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", quoteIdentifier(name))
}

// formatTableCheckConstraint adds CONSTRAINT name CHECK (...).
func formatTableCheckConstraint(name, pgConstraintDef string) string {
	def := strings.TrimSpace(pgConstraintDef)
	if strings.HasPrefix(strings.ToUpper(def), "CHECK") {
		return fmt.Sprintf("CONSTRAINT %s %s", quoteIdentifier(name), def)
	}
	return fmt.Sprintf("CONSTRAINT %s CHECK (%s)", quoteIdentifier(name), def)
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

// formatCreateView emits CREATE [MATERIALIZED] VIEW.
func formatCreateView(schema, name, definition string, materialized bool) string {
	kind := "VIEW"
	if materialized {
		kind = "MATERIALIZED VIEW"
	}
	def := strings.TrimSpace(definition)
	if strings.HasSuffix(def, ";") {
		def = strings.TrimSuffix(def, ";")
	}
	return fmt.Sprintf(
		"CREATE %s %s AS %s",
		kind,
		quoteQualifiedTable(schema, name),
		def,
	)
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

// formatGrantSchema emits GRANT privileges ON SCHEMA.
func formatGrantSchema(privileges, schema, grantee string) string {
	return fmt.Sprintf(
		"GRANT %s ON SCHEMA %s TO %s",
		privileges,
		quoteIdentifier(schema),
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
	if !pol.permissive {
		parts = append(parts, "AS RESTRICTIVE")
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
