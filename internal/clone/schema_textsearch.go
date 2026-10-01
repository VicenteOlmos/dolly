package clone

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

type textSearchDictionary struct {
	schema     string
	name       string
	tmplSchema string
	tmplName   string
	initOption string
}

type textSearchConfiguration struct {
	schema       string
	name         string
	parserSchema string
	parserName   string
}

type textSearchMapping struct {
	schema string
	name   string
	token  string
	dicts  []qualifiedName
}

type qualifiedName struct {
	schema string
	name   string
}

func loadTextSearchDictionaries(ctx context.Context, q *sql.DB, schemas []string) ([]textSearchDictionary, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, d.dictname,
		       tn.nspname, t.tmplname,
		       COALESCE(d.dictinitoption, '')
		FROM pg_ts_dict d
		JOIN pg_namespace n ON n.oid = d.dictnamespace
		JOIN pg_ts_template t ON t.oid = d.dicttemplate
		JOIN pg_namespace tn ON tn.oid = t.tmplnamespace
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend dep
		    WHERE dep.classid = 'pg_ts_dict'::regclass
		      AND dep.objid = d.oid
		      AND dep.deptype = 'e'
		  )
		ORDER BY n.nspname, d.dictname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list text search dictionaries: %w", err)
	}
	defer rows.Close()

	var out []textSearchDictionary
	for rows.Next() {
		var d textSearchDictionary
		if err := rows.Scan(&d.schema, &d.name, &d.tmplSchema, &d.tmplName, &d.initOption); err != nil {
			return nil, fmt.Errorf("scan text search dictionary: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list text search dictionaries: %w", err)
	}
	return out, nil
}

func loadTextSearchConfigurations(ctx context.Context, q *sql.DB, schemas []string) ([]textSearchConfiguration, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.cfgname,
		       pn.nspname, p.prsname
		FROM pg_ts_config c
		JOIN pg_namespace n ON n.oid = c.cfgnamespace
		JOIN pg_parser p ON p.oid = c.cfgparser
		JOIN pg_namespace pn ON pn.oid = p.prsnamespace
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend dep
		    WHERE dep.classid = 'pg_ts_config'::regclass
		      AND dep.objid = c.oid
		      AND dep.deptype = 'e'
		  )
		ORDER BY n.nspname, c.cfgname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list text search configurations: %w", err)
	}
	defer rows.Close()

	var out []textSearchConfiguration
	for rows.Next() {
		var c textSearchConfiguration
		if err := rows.Scan(&c.schema, &c.name, &c.parserSchema, &c.parserName); err != nil {
			return nil, fmt.Errorf("scan text search configuration: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list text search configurations: %w", err)
	}
	return out, nil
}

func loadTextSearchMappings(ctx context.Context, q *sql.DB, schemas []string) ([]textSearchMapping, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT cn.nspname, c.cfgname, tt.alias,
		       COALESCE(dn.nspname, ''), COALESCE(d.dictname, ''),
		       m.mapordering, m.maptokentype
		FROM pg_ts_config_map m
		JOIN pg_ts_config c ON c.oid = m.mapcfg
		JOIN pg_namespace cn ON cn.oid = c.cfgnamespace
		JOIN pg_ts_token_type tt ON tt.tokentype = m.maptokentype AND tt.prsparser = c.cfgparser
		LEFT JOIN pg_ts_dict d ON d.oid = m.mapdict AND m.mapdict <> 0
		LEFT JOIN pg_namespace dn ON dn.oid = d.dictnamespace
		WHERE cn.nspname IN (%s)
		  AND cn.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend dep
		    WHERE dep.classid = 'pg_ts_config'::regclass
		      AND dep.objid = c.oid
		      AND dep.deptype = 'e'
		  )
		ORDER BY cn.nspname, c.cfgname, m.maptokentype, m.mapordering`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list text search mappings: %w", err)
	}
	defer rows.Close()

	var out []textSearchMapping
	var cur *textSearchMapping
	var lastToken int
	for rows.Next() {
		var schema, name, token, dictSchema, dictName string
		var ordering, tokenType int
		if err := rows.Scan(&schema, &name, &token, &dictSchema, &dictName, &ordering, &tokenType); err != nil {
			return nil, fmt.Errorf("scan text search mapping: %w", err)
		}
		if cur == nil || cur.schema != schema || cur.name != name || lastToken != tokenType {
			out = append(out, textSearchMapping{schema: schema, name: name, token: token})
			cur = &out[len(out)-1]
			lastToken = tokenType
		}
		if dictName != "" {
			cur.dicts = append(cur.dicts, qualifiedName{schema: dictSchema, name: dictName})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list text search mappings: %w", err)
	}
	return out, nil
}

func applyTextSearchDictionaries(ctx context.Context, tgt execer, dicts []textSearchDictionary) error {
	for _, d := range dicts {
		stmt := formatCreateTextSearchDictionary(d)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create text search dictionary %s.%s: %w", d.schema, d.name, err)
		}
	}
	return nil
}

func applyTextSearchConfigurations(ctx context.Context, tgt execer, configs []textSearchConfiguration) error {
	for _, c := range configs {
		stmt := formatCreateTextSearchConfiguration(c)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create text search configuration %s.%s: %w", c.schema, c.name, err)
		}
	}
	return nil
}

func applyTextSearchMappings(ctx context.Context, tgt execer, mappings []textSearchMapping) error {
	for _, m := range mappings {
		if len(m.dicts) == 0 {
			continue
		}
		stmt := formatAlterTextSearchConfigurationMapping(m)
		if _, err := tgt.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("alter text search configuration %s.%s: %w", m.schema, m.name, err)
		}
	}
	return nil
}

func formatCreateTextSearchDictionary(d textSearchDictionary) string {
	opts := []string{"TEMPLATE = " + formatCatalogObjectName(d.tmplSchema, d.tmplName)}
	for _, opt := range formatTextSearchDictionaryOptions(d.initOption) {
		opts = append(opts, opt)
	}
	return fmt.Sprintf(
		"CREATE TEXT SEARCH DICTIONARY %s (%s)",
		quoteQualifiedType(d.schema, d.name),
		strings.Join(opts, ", "),
	)
}

func formatCreateTextSearchConfiguration(c textSearchConfiguration) string {
	return fmt.Sprintf(
		"CREATE TEXT SEARCH CONFIGURATION %s (PARSER = %s)",
		quoteQualifiedType(c.schema, c.name),
		formatCatalogObjectName(c.parserSchema, c.parserName),
	)
}

func formatAlterTextSearchConfigurationMapping(m textSearchMapping) string {
	dicts := make([]string, 0, len(m.dicts))
	for _, d := range m.dicts {
		dicts = append(dicts, formatCatalogObjectName(d.schema, d.name))
	}
	return fmt.Sprintf(
		"ALTER TEXT SEARCH CONFIGURATION %s ADD MAPPING FOR %s WITH %s",
		quoteQualifiedType(m.schema, m.name),
		m.token,
		strings.Join(dicts, ", "),
	)
}

func formatCatalogObjectName(schema, name string) string {
	if schema == "" || strings.EqualFold(schema, "pg_catalog") {
		return quoteIdentifier(name)
	}
	return quoteQualifiedType(schema, name)
}

func formatTextSearchDictionaryOptions(initOption string) []string {
	initOption = strings.TrimSpace(initOption)
	if initOption == "" {
		return nil
	}
	var out []string
	for _, part := range splitDictionaryOptions(initOption) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out = append(out, key+" = "+formatDictionaryOptionValue(value))
	}
	return out
}

func splitDictionaryOptions(initOption string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(initOption); i++ {
		ch := initOption[i]
		switch ch {
		case '\'':
			inQuote = !inQuote
			cur.WriteByte(ch)
		case ',':
			if inQuote {
				cur.WriteByte(ch)
				continue
			}
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func formatDictionaryOptionValue(value string) string {
	if strings.HasPrefix(value, "'") {
		return value
	}
	if isDictionaryIdentifier(value) {
		return value
	}
	return quoteLiteral(value)
}

func isDictionaryIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if r == '.' {
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}
