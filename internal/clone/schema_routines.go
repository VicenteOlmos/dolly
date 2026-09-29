package clone

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

type routineRow struct {
	oid  int64
	def  string
	name string
}

type depEdge struct {
	obj int64
	ref int64
}

func loadRoutines(ctx context.Context, q *sql.DB, schemas []string) ([]routineRow, error) {
	inClause, args := schemaINClause(schemas)
	aggregateQuery := fmt.Sprintf(`
		SELECT format('%%I.%%I(%%s)', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid))
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname IN (%s) AND p.prokind = 'a'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
		ORDER BY n.nspname, p.proname LIMIT 1`, inClause)
	var aggregate string
	switch err := q.QueryRowContext(ctx, aggregateQuery, args...).Scan(&aggregate); err {
	case nil:
		return nil, fmt.Errorf("catalog schema replay does not support aggregate %s; install pg_dump to clone this schema", aggregate)
	case sql.ErrNoRows:
	default:
		return nil, fmt.Errorf("list aggregates: %w", err)
	}
	query := fmt.Sprintf(`
		SELECT p.oid, n.nspname || '.' || p.proname, pg_get_functiondef(p.oid)
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname IN (%s)
		  AND p.prokind IN ('f', 'p', 'w')
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.objid = p.oid AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, p.proname, p.oid`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list functions: %w", err)
	}
	defer rows.Close()
	var out []routineRow
	for rows.Next() {
		var row routineRow
		if err := rows.Scan(&row.oid, &row.name, &row.def); err != nil {
			return nil, fmt.Errorf("scan function: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list functions: %w", err)
	}
	return out, nil
}

func loadRoutineDeps(ctx context.Context, q *sql.DB, schemas []string) ([]depEdge, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT p.oid, ref.oid
		FROM pg_depend d
		JOIN pg_proc p ON p.oid = d.objid
		JOIN pg_proc ref ON ref.oid = d.refobjid
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE d.deptype = 'n'
		  AND n.nspname IN (%s)
		  AND p.oid <> ref.oid`, inClause)
	return loadDepEdges(ctx, q, query, args, "function dependencies")
}

func loadViewDeps(ctx context.Context, q *sql.DB, schemas []string) (map[string][]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT nv.nspname, v.relname, nr.nspname, r.relname
		FROM pg_depend d
		JOIN pg_rewrite rw ON rw.oid = d.objid
		JOIN pg_class v ON v.oid = rw.ev_class
		JOIN pg_class r ON r.oid = d.refobjid
		JOIN pg_namespace nv ON nv.oid = v.relnamespace
		JOIN pg_namespace nr ON nr.oid = r.relnamespace
		WHERE d.classid = 'pg_rewrite'::regclass
		  AND r.relkind IN ('v', 'm')
		  AND v.oid <> r.oid
		  AND nv.nspname IN (%s)`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list view dependencies: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var schema, name, refSchema, refName string
		if err := rows.Scan(&schema, &name, &refSchema, &refName); err != nil {
			return nil, fmt.Errorf("scan view dependency: %w", err)
		}
		key := schema + "." + name
		ref := refSchema + "." + refName
		out[key] = append(out[key], ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list view dependencies: %w", err)
	}
	return out, nil
}

func loadTriggers(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, t.tgname, t.tgenabled, pg_get_triggerdef(t.oid, true)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE NOT t.tgisinternal
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, t.tgname`, inClause)
	return loadEnabledDefs(ctx, q, query, args, "triggers", "TRIGGER")
}

func loadRules(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, r.rulename, r.ev_enabled, pg_get_ruledef(r.oid, true)
		FROM pg_rewrite r
		JOIN pg_class c ON c.oid = r.ev_class
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE r.rulename <> '_RETURN'
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, r.rulename`, inClause)
	return loadEnabledDefs(ctx, q, query, args, "rules", "RULE")
}

func loadEnabledDefs(ctx context.Context, q *sql.DB, query string, args []any, label, kind string) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", label, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var schema, table, name, mode, def string
		if err := rows.Scan(&schema, &table, &name, &mode, &def); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, def)
		if mode != "O" {
			verb := map[string]string{"D": "DISABLE", "R": "ENABLE REPLICA", "A": "ENABLE ALWAYS"}[mode]
			if verb == "" {
				return nil, fmt.Errorf("unknown %s mode %q for %s.%s.%s", kind, mode, schema, table, name)
			}
			out = append(out, fmt.Sprintf("ALTER TABLE %s %s %s %s", quoteQualifiedTable(schema, table), verb, kind, quoteIdentifier(name)))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s: %w", label, err)
	}
	return out, nil
}

func loadDepEdges(ctx context.Context, q *sql.DB, query string, args []any, label string) ([]depEdge, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", label, err)
	}
	defer rows.Close()
	var out []depEdge
	for rows.Next() {
		var edge depEdge
		if err := rows.Scan(&edge.obj, &edge.ref); err != nil {
			return nil, fmt.Errorf("scan %s: %w", label, err)
		}
		out = append(out, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list %s: %w", label, err)
	}
	return out, nil
}

func applySQLDefs(ctx context.Context, tgt *sql.DB, defs []string, label string) error {
	for _, def := range defs {
		if _, err := tgt.ExecContext(ctx, def); err != nil {
			return fmt.Errorf("apply %s: %w", label, err)
		}
	}
	return nil
}

func orderRoutines(rows []routineRow, edges []depEdge) ([]routineRow, error) {
	byOID := make(map[int64]routineRow, len(rows))
	indegree := make(map[int64]int, len(rows))
	next := make(map[int64][]int64)
	for _, row := range rows {
		byOID[row.oid] = row
		indegree[row.oid] = 0
	}
	for _, edge := range edges {
		if _, ok := byOID[edge.obj]; !ok {
			continue
		}
		if _, ok := byOID[edge.ref]; !ok {
			continue
		}
		next[edge.ref] = append(next[edge.ref], edge.obj)
		indegree[edge.obj]++
	}
	ready := make([]int64, 0, len(rows))
	for oid, deg := range indegree {
		if deg == 0 {
			ready = append(ready, oid)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return byOID[ready[i]].name < byOID[ready[j]].name })
	var ordered []routineRow
	for len(ready) > 0 {
		oid := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byOID[oid])
		kids := append([]int64(nil), next[oid]...)
		sort.Slice(kids, func(i, j int) bool { return byOID[kids[i]].name < byOID[kids[j]].name })
		for _, kid := range kids {
			indegree[kid]--
			if indegree[kid] == 0 {
				ready = append(ready, kid)
			}
		}
	}
	if len(ordered) != len(rows) {
		return nil, fmt.Errorf("cyclic function dependency")
	}
	return ordered, nil
}

func orderViews(views []viewRow, deps map[string][]string) ([]viewRow, error) {
	byKey := make(map[string]viewRow, len(views))
	indegree := make(map[string]int, len(views))
	next := make(map[string][]string)
	for _, view := range views {
		key := view.schema + "." + view.name
		byKey[key] = view
		indegree[key] = 0
	}
	for key, refs := range deps {
		if _, ok := byKey[key]; !ok {
			continue
		}
		for _, ref := range refs {
			if _, ok := byKey[ref]; !ok {
				continue
			}
			next[ref] = append(next[ref], key)
			indegree[key]++
		}
	}
	ready := make([]string, 0, len(views))
	for key, deg := range indegree {
		if deg == 0 {
			ready = append(ready, key)
		}
	}
	sort.Strings(ready)
	var ordered []viewRow
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		ordered = append(ordered, byKey[key])
		kids := append([]string(nil), next[key]...)
		sort.Strings(kids)
		for _, kid := range kids {
			indegree[kid]--
			if indegree[kid] == 0 {
				ready = append(ready, kid)
			}
		}
	}
	if len(ordered) != len(views) {
		return nil, fmt.Errorf("cyclic view dependency")
	}
	return ordered, nil
}
