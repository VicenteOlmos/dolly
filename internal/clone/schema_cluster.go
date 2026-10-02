package clone

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// applyClusterGlobalsDB creates missing roles and tablespaces on an open target.
// Tests replace it so schema-replay execution tests do not query a mock database.
var applyClusterGlobalsDB = applyClusterGlobals

// applyClusterGlobalsFn opens source and target DSNs and creates missing cluster objects.
// The pg_dump schema-replay path calls it before piping schema SQL.
var applyClusterGlobalsFn = openAndApplyClusterGlobals

func openAndApplyClusterGlobals(ctx context.Context, srcDSN, tgtDSN string) error {
	src, err := sqlOpenDB(srcDSN)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()
	tgt, err := sqlOpenDB(tgtDSN)
	if err != nil {
		return fmt.Errorf("open target: %w", err)
	}
	defer tgt.Close()
	return applyClusterGlobals(ctx, src, tgt)
}

type roleSpec struct {
	name       string
	super      bool
	inherit    bool
	createRole bool
	createDB   bool
	login      bool
	replic     bool
	bypassRLS  bool
	connLimit  int
	validUntil sql.NullString
	comment    sql.NullString
	password   sql.NullString
}

type roleGrant struct {
	parent  string
	member  string
	admin   bool
	inherit bool
	set     bool
}

type tablespaceSpec struct {
	name     string
	owner    string
	location string
	comment  string
	options  map[string]string
}

func applyClusterGlobals(ctx context.Context, src, tgt *sql.DB) error {
	srcMajor, err := scanServerMajor(ctx, src)
	if err != nil {
		return fmt.Errorf("source version: %w", err)
	}
	tgtMajor, err := scanServerMajor(ctx, tgt)
	if err != nil {
		return fmt.Errorf("target version: %w", err)
	}
	roles, err := loadUserRoles(ctx, src)
	if err != nil {
		return err
	}
	grants, err := loadRoleGrants(ctx, src, srcMajor)
	if err != nil {
		return err
	}
	tablespaces, err := loadUserTablespaces(ctx, src)
	if err != nil {
		return err
	}
	created := map[string]struct{}{}
	for _, role := range roles {
		ok, err := ensureRole(ctx, tgt, role)
		if err != nil {
			return err
		}
		if ok {
			created[role.name] = struct{}{}
		}
	}
	for _, grant := range grants {
		if _, ok := created[grant.member]; !ok {
			continue
		}
		exists, err := roleMembershipExists(ctx, tgt, grant.parent, grant.member)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := tgt.ExecContext(ctx, formatGrantRole(grant, tgtMajor)); err != nil {
			return fmt.Errorf("grant role %s to %s: %w", grant.parent, grant.member, err)
		}
	}
	for _, ts := range tablespaces {
		if err := ensureTablespace(ctx, tgt, ts); err != nil {
			return err
		}
	}
	return nil
}

// ensureRole creates role when the target has no role of that name.
// An existing role is left unchanged and does not require CREATEROLE.
func ensureRole(ctx context.Context, tgt *sql.DB, role roleSpec) (bool, error) {
	var exists bool
	if err := tgt.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role.name).Scan(&exists); err != nil {
		return false, fmt.Errorf("check role %s: %w", role.name, err)
	}
	if exists {
		return false, nil
	}
	if _, err := tgt.ExecContext(ctx, formatEnsureRole(role)); err != nil {
		return false, fmt.Errorf("create role %s: %w", role.name, err)
	}
	return true, nil
}

func roleMembershipExists(ctx context.Context, tgt *sql.DB, parent, member string) (bool, error) {
	var exists bool
	err := tgt.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_auth_members m
			JOIN pg_roles granted ON granted.oid = m.roleid
			JOIN pg_roles member ON member.oid = m.member
			WHERE granted.rolname = $1 AND member.rolname = $2
		)`, parent, member).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check membership %s to %s: %w", parent, member, err)
	}
	return exists, nil
}

func loadUserRoles(ctx context.Context, q *sql.DB) ([]roleSpec, error) {
	const query = `
		SELECT r.rolname, r.rolsuper, r.rolinherit, r.rolcreaterole, r.rolcreatedb,
		       r.rolcanlogin, r.rolreplication, r.rolbypassrls, r.rolconnlimit,
		       r.rolvaliduntil::text,
		       pg_catalog.shobj_description(r.oid, 'pg_authid')
		FROM pg_roles r
		WHERE r.oid >= 16384
		ORDER BY r.rolname`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var out []roleSpec
	for rows.Next() {
		var role roleSpec
		if err := rows.Scan(
			&role.name, &role.super, &role.inherit, &role.createRole, &role.createDB,
			&role.login, &role.replic, &role.bypassRLS, &role.connLimit,
			&role.validUntil, &role.comment,
		); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	passwords, err := loadRolePasswords(ctx, q)
	if err != nil {
		return nil, err
	}
	if passwords == nil {
		return out, nil
	}
	for i := range out {
		if pw, ok := passwords[out[i].name]; ok {
			out[i].password = pw
		}
	}
	return out, nil
}

func loadRolePasswords(ctx context.Context, q *sql.DB) (map[string]sql.NullString, error) {
	const query = `SELECT rolname, rolpassword FROM pg_authid WHERE oid >= 16384`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		if isInsufficientPrivilege(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list role passwords: %w", err)
	}
	defer rows.Close()
	out := map[string]sql.NullString{}
	for rows.Next() {
		var name string
		var password sql.NullString
		if err := rows.Scan(&name, &password); err != nil {
			return nil, fmt.Errorf("scan role password: %w", err)
		}
		out[name] = password
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list role passwords: %w", err)
	}
	return out, nil
}

func loadRoleGrants(ctx context.Context, q *sql.DB, major int) ([]roleGrant, error) {
	// inherit_option and set_option arrived in PostgreSQL 16. Earlier majors
	// store only admin_option; role-level INHERIT stays on pg_roles.rolinherit.
	query := `
		SELECT granted.rolname, member.rolname, m.admin_option
		FROM pg_auth_members m
		JOIN pg_roles granted ON granted.oid = m.roleid
		JOIN pg_roles member ON member.oid = m.member
		WHERE member.oid >= 16384
		ORDER BY granted.rolname, member.rolname`
	if major >= 16 {
		query = `
		SELECT granted.rolname, member.rolname, m.admin_option, m.inherit_option, m.set_option
		FROM pg_auth_members m
		JOIN pg_roles granted ON granted.oid = m.roleid
		JOIN pg_roles member ON member.oid = m.member
		WHERE member.oid >= 16384
		ORDER BY granted.rolname, member.rolname`
	}
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list role grants: %w", err)
	}
	defer rows.Close()
	var out []roleGrant
	for rows.Next() {
		var grant roleGrant
		var err error
		if major >= 16 {
			err = rows.Scan(&grant.parent, &grant.member, &grant.admin, &grant.inherit, &grant.set)
		} else {
			err = rows.Scan(&grant.parent, &grant.member, &grant.admin)
			// PostgreSQL 16 GRANT defaults. Pre-16 memberships have no separate flags.
			grant.inherit = true
			grant.set = true
		}
		if err != nil {
			return nil, fmt.Errorf("scan role grant: %w", err)
		}
		out = append(out, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list role grants: %w", err)
	}
	return out, nil
}

func loadUserTablespaces(ctx context.Context, q *sql.DB) ([]tablespaceSpec, error) {
	const query = `
		SELECT t.spcname, r.rolname, pg_catalog.pg_tablespace_location(t.oid),
		       COALESCE(pg_catalog.shobj_description(t.oid, 'pg_tablespace'), '')
		FROM pg_tablespace t
		JOIN pg_roles r ON r.oid = t.spcowner
		WHERE t.spcname NOT IN ('pg_default', 'pg_global')
		ORDER BY t.spcname`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tablespaces: %w", err)
	}
	defer rows.Close()
	byName := map[string]*tablespaceSpec{}
	var order []string
	for rows.Next() {
		var ts tablespaceSpec
		if err := rows.Scan(&ts.name, &ts.owner, &ts.location, &ts.comment); err != nil {
			return nil, fmt.Errorf("scan tablespace: %w", err)
		}
		if strings.TrimSpace(ts.location) == "" {
			return nil, fmt.Errorf("tablespace %s has no location", ts.name)
		}
		ts.options = map[string]string{}
		byName[ts.name] = &ts
		order = append(order, ts.name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tablespaces: %w", err)
	}
	const optQuery = `
		SELECT t.spcname, opt.option_name, opt.option_value
		FROM pg_tablespace t
		CROSS JOIN LATERAL pg_catalog.pg_options_to_table(t.spcoptions) opt
		WHERE t.spcname NOT IN ('pg_default', 'pg_global')
		  AND t.spcoptions IS NOT NULL
		ORDER BY t.spcname, opt.option_name`
	optRows, err := q.QueryContext(ctx, optQuery)
	if err != nil {
		return nil, fmt.Errorf("list tablespace options: %w", err)
	}
	defer optRows.Close()
	for optRows.Next() {
		var name, key, value string
		if err := optRows.Scan(&name, &key, &value); err != nil {
			return nil, fmt.Errorf("scan tablespace option: %w", err)
		}
		if ts, ok := byName[name]; ok {
			ts.options[key] = value
		}
	}
	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("list tablespace options: %w", err)
	}
	out := make([]tablespaceSpec, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

func ensureTablespace(ctx context.Context, tgt *sql.DB, ts tablespaceSpec) error {
	var exists bool
	if err := tgt.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tablespace WHERE spcname = $1)`, ts.name).Scan(&exists); err != nil {
		return fmt.Errorf("check tablespace %s: %w", ts.name, err)
	}
	if exists {
		return nil
	}
	if _, err := tgt.ExecContext(ctx, formatCreateTablespace(ts)); err != nil {
		return fmt.Errorf("create tablespace %s: %w", ts.name, err)
	}
	if ts.comment != "" {
		stmt := fmt.Sprintf("COMMENT ON TABLESPACE %s IS %s", quoteIdentifier(ts.name), quoteLiteral(ts.comment))
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("comment on tablespace %s: %w", ts.name, err)
		}
	}
	return nil
}

func formatEnsureRole(role roleSpec) string {
	attrs := []string{
		flagWord(role.super, "SUPERUSER", "NOSUPERUSER"),
		flagWord(role.inherit, "INHERIT", "NOINHERIT"),
		flagWord(role.createRole, "CREATEROLE", "NOCREATEROLE"),
		flagWord(role.createDB, "CREATEDB", "NOCREATEDB"),
		flagWord(role.login, "LOGIN", "NOLOGIN"),
		flagWord(role.replic, "REPLICATION", "NOREPLICATION"),
		flagWord(role.bypassRLS, "BYPASSRLS", "NOBYPASSRLS"),
		fmt.Sprintf("CONNECTION LIMIT %d", role.connLimit),
	}
	if role.password.Valid {
		attrs = append(attrs, "PASSWORD "+quoteLiteral(role.password.String))
	}
	if role.validUntil.Valid && role.validUntil.String != "" {
		attrs = append(attrs, "VALID UNTIL "+quoteLiteral(role.validUntil.String))
	}
	name := quoteIdentifier(role.name)
	stmts := []string{
		"CREATE ROLE " + name,
		"ALTER ROLE " + name + " WITH " + strings.Join(attrs, " "),
	}
	if role.comment.Valid && role.comment.String != "" {
		stmts = append(stmts, "COMMENT ON ROLE "+name+" IS "+quoteLiteral(role.comment.String))
	}
	return wrapDuplicateObject(stmts...)
}

func formatGrantRole(grant roleGrant, major int) string {
	parent := quoteIdentifier(grant.parent)
	member := quoteIdentifier(grant.member)
	if major < 16 {
		stmt := fmt.Sprintf("GRANT %s TO %s", parent, member)
		if grant.admin {
			stmt += " WITH ADMIN OPTION"
		}
		return stmt
	}
	return fmt.Sprintf(
		"GRANT %s TO %s WITH ADMIN %s, INHERIT %s, SET %s",
		parent,
		member,
		boolWord(grant.admin),
		boolWord(grant.inherit),
		boolWord(grant.set),
	)
}

func formatCreateTablespace(ts tablespaceSpec) string {
	stmt := fmt.Sprintf(
		"CREATE TABLESPACE %s OWNER %s LOCATION %s",
		quoteIdentifier(ts.name), quoteIdentifier(ts.owner), quoteLiteral(ts.location),
	)
	if len(ts.options) == 0 {
		return stmt
	}
	keys := make([]string, 0, len(ts.options))
	for key := range ts.options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+formatTablespaceOptionValue(ts.options[key]))
	}
	return stmt + " WITH (" + strings.Join(parts, ", ") + ")"
}

var tablespaceNumber = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

func formatTablespaceOptionValue(value string) string {
	if tablespaceNumber.MatchString(value) {
		return value
	}
	return quoteLiteral(value)
}

func flagWord(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

func boolWord(on bool) string {
	if on {
		return "TRUE"
	}
	return "FALSE"
}

type relationTablespace struct {
	schema string
	name   string
	kind   string
	spc    string
}

func viewsIncludeMaterialized(views []viewRow) bool {
	for _, view := range views {
		if view.materialized {
			return true
		}
	}
	return false
}

func applyRelationTablespaces(ctx context.Context, src *sql.DB, tgt execer, schemas []string, kinds ...string) error {
	rows, err := loadRelationTablespaces(ctx, src, schemas, kinds)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var stmt string
		switch row.kind {
		case "m":
			stmt = fmt.Sprintf("ALTER MATERIALIZED VIEW %s SET TABLESPACE %s", quoteQualifiedTable(row.schema, row.name), quoteIdentifier(row.spc))
		default:
			stmt = fmt.Sprintf("ALTER TABLE %s SET TABLESPACE %s", quoteQualifiedTable(row.schema, row.name), quoteIdentifier(row.spc))
		}
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("set tablespace on %s.%s: %w", row.schema, row.name, err)
		}
	}
	return nil
}

func loadRelationTablespaces(ctx context.Context, q *sql.DB, schemas []string, kinds []string) ([]relationTablespace, error) {
	inClause, args := schemaINClause(schemas)
	kindList := make([]string, len(kinds))
	for i, kind := range kinds {
		if kind != "r" && kind != "p" && kind != "m" {
			return nil, fmt.Errorf("unsupported relation kind %q", kind)
		}
		kindList[i] = quoteLiteral(kind)
	}
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, c.relkind::text, t.spcname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_tablespace t ON t.oid = c.reltablespace
		WHERE c.reltablespace <> 0
		  AND c.relkind::text IN (%s)
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname`, strings.Join(kindList, ", "), inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list relation tablespaces: %w", err)
	}
	defer rows.Close()
	var out []relationTablespace
	for rows.Next() {
		var row relationTablespace
		if err := rows.Scan(&row.schema, &row.name, &row.kind, &row.spc); err != nil {
			return nil, fmt.Errorf("scan relation tablespace: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list relation tablespaces: %w", err)
	}
	return out, nil
}
