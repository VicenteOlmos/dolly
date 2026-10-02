package clone

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type foreignServerDef struct {
	name     string
	fdw      string
	srvType  string
	version  string
	owner    string
	options  map[string]string
	fdwOwned bool
}

type foreignDataWrapperDef struct {
	name       string
	handler    string
	validator  string
	options    map[string]string
	hasHandler bool
	hasValid   bool
}

type userMappingDef struct {
	server  string
	user    string // empty means PUBLIC
	options map[string]string
}

func applyForeignServers(ctx context.Context, srcDB *sql.DB, tgtDB execer, tables []foreignTableDef) error {
	names := foreignServerNames(tables)
	if len(names) == 0 {
		return nil
	}
	servers, err := loadForeignServers(ctx, srcDB, names)
	if err != nil {
		return err
	}
	wrappers, err := loadLooseForeignDataWrappers(ctx, srcDB, servers)
	if err != nil {
		return err
	}
	if err := applySQLDefs(ctx, tgtDB, formatForeignDataWrappers(wrappers), "foreign data wrapper"); err != nil {
		return err
	}
	if err := applySQLDefs(ctx, tgtDB, formatForeignServers(servers), "foreign server"); err != nil {
		return err
	}
	mappings, err := loadUserMappings(ctx, srcDB, names)
	if err != nil {
		return err
	}
	return applySQLDefs(ctx, tgtDB, formatUserMappings(mappings), "user mapping")
}

func foreignServerNames(tables []foreignTableDef) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, ft := range tables {
		if ft.server == "" {
			continue
		}
		if _, ok := seen[ft.server]; ok {
			continue
		}
		seen[ft.server] = struct{}{}
		names = append(names, ft.server)
	}
	sort.Strings(names)
	return names
}

func loadForeignServers(ctx context.Context, q *sql.DB, names []string) ([]foreignServerDef, error) {
	inClause, args := schemaINClause(names)
	query := fmt.Sprintf(`
		SELECT srv.srvname, fdw.fdwname, COALESCE(srv.srvtype, ''), COALESCE(srv.srvversion, ''), owner.rolname,
		       EXISTS (
		         SELECT 1 FROM pg_depend d
		         WHERE d.classid = 'pg_foreign_data_wrapper'::regclass
		           AND d.objid = fdw.oid AND d.deptype = 'e'
		       )
		FROM pg_foreign_server srv
		JOIN pg_foreign_data_wrapper fdw ON fdw.oid = srv.srvfdw
		JOIN pg_roles owner ON owner.oid = srv.srvowner
		WHERE srv.srvname IN (%s)
		ORDER BY srv.srvname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign servers: %w", err)
	}
	defer rows.Close()
	byName := map[string]*foreignServerDef{}
	var order []string
	for rows.Next() {
		var srv foreignServerDef
		if err := rows.Scan(&srv.name, &srv.fdw, &srv.srvType, &srv.version, &srv.owner, &srv.fdwOwned); err != nil {
			return nil, fmt.Errorf("scan foreign server: %w", err)
		}
		srv.options = map[string]string{}
		byName[srv.name] = &srv
		order = append(order, srv.name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign servers: %w", err)
	}
	for _, name := range names {
		if _, ok := byName[name]; !ok {
			return nil, fmt.Errorf("foreign server %q not found", name)
		}
	}
	optQuery := fmt.Sprintf(`
		SELECT srv.srvname, opt.option_name, opt.option_value
		FROM pg_foreign_server srv
		CROSS JOIN LATERAL pg_catalog.pg_options_to_table(srv.srvoptions) opt
		WHERE srv.srvname IN (%s) AND srv.srvoptions IS NOT NULL
		ORDER BY srv.srvname, opt.option_name`, inClause)
	optRows, err := q.QueryContext(ctx, optQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign server options: %w", err)
	}
	defer optRows.Close()
	for optRows.Next() {
		var name, key, value string
		if err := optRows.Scan(&name, &key, &value); err != nil {
			return nil, fmt.Errorf("scan foreign server option: %w", err)
		}
		if srv, ok := byName[name]; ok {
			srv.options[key] = value
		}
	}
	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign server options: %w", err)
	}
	out := make([]foreignServerDef, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

func loadLooseForeignDataWrappers(ctx context.Context, q *sql.DB, servers []foreignServerDef) ([]foreignDataWrapperDef, error) {
	seen := map[string]struct{}{}
	var names []string
	for _, srv := range servers {
		if srv.fdwOwned {
			continue
		}
		if _, ok := seen[srv.fdw]; ok {
			continue
		}
		seen[srv.fdw] = struct{}{}
		names = append(names, srv.fdw)
	}
	if len(names) == 0 {
		return nil, nil
	}
	inClause, args := schemaINClause(names)
	query := fmt.Sprintf(`
		SELECT fdw.fdwname,
		       fdw.fdwhandler <> 0, COALESCE(hn.nspname, ''), COALESCE(h.proname, ''),
		       fdw.fdwvalidator <> 0, COALESCE(vn.nspname, ''), COALESCE(v.proname, '')
		FROM pg_foreign_data_wrapper fdw
		LEFT JOIN pg_proc h ON fdw.fdwhandler <> 0 AND h.oid = fdw.fdwhandler
		LEFT JOIN pg_namespace hn ON hn.oid = h.pronamespace
		LEFT JOIN pg_proc v ON fdw.fdwvalidator <> 0 AND v.oid = fdw.fdwvalidator
		LEFT JOIN pg_namespace vn ON vn.oid = v.pronamespace
		WHERE fdw.fdwname IN (%s)
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_foreign_data_wrapper'::regclass
		      AND d.objid = fdw.oid AND d.deptype = 'e'
		  )
		ORDER BY fdw.fdwname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign data wrappers: %w", err)
	}
	defer rows.Close()
	byName := map[string]*foreignDataWrapperDef{}
	var order []string
	for rows.Next() {
		var w foreignDataWrapperDef
		var handlerSchema, handlerName, validSchema, validName string
		if err := rows.Scan(&w.name, &w.hasHandler, &handlerSchema, &handlerName, &w.hasValid, &validSchema, &validName); err != nil {
			return nil, fmt.Errorf("scan foreign data wrapper: %w", err)
		}
		if w.hasHandler {
			w.handler = quoteQualifiedTable(handlerSchema, handlerName)
		}
		if w.hasValid {
			w.validator = quoteQualifiedTable(validSchema, validName)
		}
		w.options = map[string]string{}
		byName[w.name] = &w
		order = append(order, w.name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign data wrappers: %w", err)
	}
	optQuery := fmt.Sprintf(`
		SELECT fdw.fdwname, opt.option_name, opt.option_value
		FROM pg_foreign_data_wrapper fdw
		CROSS JOIN LATERAL pg_catalog.pg_options_to_table(fdw.fdwoptions) opt
		WHERE fdw.fdwname IN (%s) AND fdw.fdwoptions IS NOT NULL
		ORDER BY fdw.fdwname, opt.option_name`, inClause)
	optRows, err := q.QueryContext(ctx, optQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("list foreign data wrapper options: %w", err)
	}
	defer optRows.Close()
	for optRows.Next() {
		var name, key, value string
		if err := optRows.Scan(&name, &key, &value); err != nil {
			return nil, fmt.Errorf("scan foreign data wrapper option: %w", err)
		}
		if w, ok := byName[name]; ok {
			w.options[key] = value
		}
	}
	if err := optRows.Err(); err != nil {
		return nil, fmt.Errorf("list foreign data wrapper options: %w", err)
	}
	out := make([]foreignDataWrapperDef, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	return out, nil
}

func loadUserMappings(ctx context.Context, q *sql.DB, servers []string) ([]userMappingDef, error) {
	inClause, args := schemaINClause(servers)
	query := fmt.Sprintf(`
		SELECT srv.srvname,
		       CASE WHEN um.umuser = 0 THEN '' ELSE r.rolname END,
		       opt.option_name, opt.option_value
		FROM pg_user_mapping um
		JOIN pg_foreign_server srv ON srv.oid = um.umserver
		LEFT JOIN pg_roles r ON r.oid = um.umuser
		LEFT JOIN LATERAL pg_catalog.pg_options_to_table(um.umoptions) opt ON true
		WHERE srv.srvname IN (%s)
		ORDER BY srv.srvname, 2, opt.option_name`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		if isInsufficientPrivilege(err) {
			return loadUserMappingsWithoutOptions(ctx, q, servers)
		}
		return nil, fmt.Errorf("list user mappings: %w", err)
	}
	defer rows.Close()
	return scanUserMappingRows(rows)
}

func loadUserMappingsWithoutOptions(ctx context.Context, q *sql.DB, servers []string) ([]userMappingDef, error) {
	inClause, args := schemaINClause(servers)
	query := fmt.Sprintf(`
		SELECT m.srvname, CASE WHEN m.umuser = 0 THEN '' ELSE COALESCE(m.usename, '') END
		FROM pg_user_mappings m
		WHERE m.srvname IN (%s)
		ORDER BY m.srvname, 2`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list user mappings: %w", err)
	}
	defer rows.Close()
	var out []userMappingDef
	for rows.Next() {
		var m userMappingDef
		if err := rows.Scan(&m.server, &m.user); err != nil {
			return nil, fmt.Errorf("scan user mapping: %w", err)
		}
		m.options = map[string]string{}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user mappings: %w", err)
	}
	return out, nil
}

func scanUserMappingRows(rows *sql.Rows) ([]userMappingDef, error) {
	byKey := map[string]*userMappingDef{}
	var order []string
	for rows.Next() {
		var server, user string
		var optName, optValue sql.NullString
		if err := rows.Scan(&server, &user, &optName, &optValue); err != nil {
			return nil, fmt.Errorf("scan user mapping: %w", err)
		}
		key := server + "\x00" + user
		m, ok := byKey[key]
		if !ok {
			m = &userMappingDef{server: server, user: user, options: map[string]string{}}
			byKey[key] = m
			order = append(order, key)
		}
		if optName.Valid && optValue.Valid {
			m.options[optName.String] = optValue.String
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user mappings: %w", err)
	}
	out := make([]userMappingDef, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	return out, nil
}

func formatForeignDataWrappers(wrappers []foreignDataWrapperDef) []string {
	out := make([]string, 0, len(wrappers))
	for _, w := range wrappers {
		out = append(out, wrapDuplicateObject(formatCreateForeignDataWrapper(w)))
	}
	return out
}

func formatCreateForeignDataWrapper(w foreignDataWrapperDef) string {
	stmt := "CREATE FOREIGN DATA WRAPPER " + quoteIdentifier(w.name)
	if w.hasHandler {
		stmt += " HANDLER " + w.handler
	} else {
		stmt += " NO HANDLER"
	}
	if w.hasValid {
		stmt += " VALIDATOR " + w.validator
	} else {
		stmt += " NO VALIDATOR"
	}
	if opt := formatFDWOptionsClause(w.options); opt != "" {
		stmt += opt
	}
	return stmt
}

func formatForeignServers(servers []foreignServerDef) []string {
	out := make([]string, 0, len(servers))
	for _, srv := range servers {
		out = append(out, wrapDuplicateObject(
			formatCreateForeignServer(srv),
			"ALTER SERVER "+quoteIdentifier(srv.name)+" OWNER TO "+quoteIdentifier(srv.owner),
		))
	}
	return out
}

func formatCreateForeignServer(srv foreignServerDef) string {
	stmt := "CREATE SERVER " + quoteIdentifier(srv.name)
	if srv.srvType != "" {
		stmt += " TYPE " + quoteLiteral(srv.srvType)
	}
	if srv.version != "" {
		stmt += " VERSION " + quoteLiteral(srv.version)
	}
	stmt += " FOREIGN DATA WRAPPER " + quoteIdentifier(srv.fdw)
	if opt := formatFDWOptionsClause(srv.options); opt != "" {
		stmt += opt
	}
	return stmt
}

func formatUserMappings(mappings []userMappingDef) []string {
	out := make([]string, 0, len(mappings))
	for _, m := range mappings {
		out = append(out, wrapDuplicateObject(formatCreateUserMapping(m)))
	}
	return out
}

func formatCreateUserMapping(m userMappingDef) string {
	who := "PUBLIC"
	if m.user != "" {
		who = quoteIdentifier(m.user)
	}
	stmt := fmt.Sprintf("CREATE USER MAPPING FOR %s SERVER %s", who, quoteIdentifier(m.server))
	if opt := formatFDWOptionsClause(m.options); opt != "" {
		stmt += opt
	}
	return stmt
}

// wrapDuplicateObject runs statements once and ignores duplicate_object.
// Each argument is one statement. Newlines inside literals stay inside that
// statement. The dollar tag is chosen so it does not occur in the body.
func wrapDuplicateObject(statements ...string) string {
	var b strings.Builder
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		for strings.HasSuffix(stmt, ";") {
			stmt = strings.TrimSpace(strings.TrimSuffix(stmt, ";"))
		}
		b.WriteString("  ")
		b.WriteString(stmt)
		b.WriteString(";\n")
	}
	body := b.String()
	tag := dollarQuoteTag(body)
	return "DO " + tag + "\nBEGIN\n" + body + "EXCEPTION WHEN duplicate_object THEN NULL;\nEND\n" + tag
}

func dollarQuoteTag(body string) string {
	tag := "$dolly$"
	for n := 0; strings.Contains(body, tag); n++ {
		tag = fmt.Sprintf("$dolly%d$", n)
	}
	return tag
}
