package clone

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
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

func loadRoutines(ctx context.Context, q *sql.DB, schemas []string) ([]routineRow, []string, error) {
	aggregates, err := loadAggregates(ctx, q, schemas)
	if err != nil {
		return nil, nil, err
	}
	inClause, args := schemaINClause(schemas)
	// Extension members (deptype e) and objects owned internally by another
	// catalog object (deptype i) are not replayed. Range and multirange
	// constructors are internal: CREATE TYPE AS RANGE creates them, and their
	// definitions mention the range array type, which does not exist on a shell.
	query := fmt.Sprintf(`
		SELECT p.oid, n.nspname || '.' || p.proname, pg_get_functiondef(p.oid)
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname IN (%s)
		  AND p.prokind IN ('f', 'p', 'w')
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_proc'::regclass
		      AND d.objid = p.oid
		      AND d.deptype IN ('e', 'i')
		  )
		ORDER BY n.nspname, p.proname, p.oid`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list functions: %w", err)
	}
	defer rows.Close()
	var out []routineRow
	for rows.Next() {
		var row routineRow
		if err := rows.Scan(&row.oid, &row.name, &row.def); err != nil {
			return nil, nil, fmt.Errorf("scan function: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list functions: %w", err)
	}
	return out, aggregates, nil
}

func loadAggregates(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), a.aggkind, a.aggnumdirectargs, p.proparallel,
		       format_type(a.aggtranstype, NULL),
		       sn.nspname, sp.proname, a.aggtransspace,
		       COALESCE(fn.nspname, ''), COALESCE(fp.proname, ''), a.aggfinalextra,
		       COALESCE(cn.nspname, ''), COALESCE(cp.proname, ''),
		       COALESCE(srn.nspname, ''), COALESCE(srp.proname, ''),
		       COALESCE(dsn.nspname, ''), COALESCE(dsp.proname, ''),
		       a.agginitval,
		       COALESCE(mn.nspname, ''), COALESCE(mp.proname, ''),
		       CASE WHEN a.aggmtranstype = 0 THEN '' ELSE format_type(a.aggmtranstype, NULL) END,
		       a.aggmtransspace, a.aggminitval,
		       COALESCE(invn.nspname, ''), COALESCE(invp.proname, ''),
		       COALESCE(mfn.nspname, ''), COALESCE(mfp.proname, ''),
		       a.aggmfinalextra
		FROM pg_aggregate a
		JOIN pg_proc p ON p.oid = a.aggfnoid
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_proc sp ON sp.oid = a.aggtransfn
		JOIN pg_namespace sn ON sn.oid = sp.pronamespace
		LEFT JOIN pg_proc fp ON a.aggfinalfn <> 0 AND fp.oid = a.aggfinalfn
		LEFT JOIN pg_namespace fn ON fn.oid = fp.pronamespace
		LEFT JOIN pg_proc cp ON a.aggcombinefn <> 0 AND cp.oid = a.aggcombinefn
		LEFT JOIN pg_namespace cn ON cn.oid = cp.pronamespace
		LEFT JOIN pg_proc srp ON a.aggserialfn <> 0 AND srp.oid = a.aggserialfn
		LEFT JOIN pg_namespace srn ON srn.oid = srp.pronamespace
		LEFT JOIN pg_proc dsp ON a.aggdeserialfn <> 0 AND dsp.oid = a.aggdeserialfn
		LEFT JOIN pg_namespace dsn ON dsn.oid = dsp.pronamespace
		LEFT JOIN pg_proc mp ON a.aggmtransfn <> 0 AND mp.oid = a.aggmtransfn
		LEFT JOIN pg_namespace mn ON mn.oid = mp.pronamespace
		LEFT JOIN pg_proc invp ON a.aggminvtransfn <> 0 AND invp.oid = a.aggminvtransfn
		LEFT JOIN pg_namespace invn ON invn.oid = invp.pronamespace
		LEFT JOIN pg_proc mfp ON a.aggmfinalfn <> 0 AND mfp.oid = a.aggmfinalfn
		LEFT JOIN pg_namespace mfn ON mfn.oid = mfp.pronamespace
		WHERE n.nspname IN (%s) AND a.aggkind IN ('n', 'o', 'h')
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
		ORDER BY n.nspname, p.proname, p.oid`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list aggregates: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var spec aggregateSpec
		var initVal, minit sql.NullString
		if err := rows.Scan(
			&spec.schema, &spec.name, &spec.args, &spec.aggkind, &spec.aggNumDirect, &spec.parallel,
			&spec.stype,
			&spec.sfuncSchema, &spec.sfunc, &spec.sspace,
			&spec.finalSchema, &spec.final, &spec.finalExtra,
			&spec.combineSchema, &spec.combine,
			&spec.serialSchema, &spec.serial,
			&spec.deserialSchema, &spec.deserial,
			&initVal,
			&spec.msfuncSchema, &spec.msfunc,
			&spec.mstype, &spec.msspace, &minit,
			&spec.minvSchema, &spec.minv,
			&spec.mfinalSchema, &spec.mfinal, &spec.mfinalExtra,
		); err != nil {
			return nil, fmt.Errorf("scan aggregate: %w", err)
		}
		spec.initVal = initVal
		spec.minit = minit
		out = append(out, formatAggregate(spec))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list aggregates: %w", err)
	}
	return out, nil
}

type aggregateSpec struct {
	schema, name, args, aggkind, parallel, stype string
	aggNumDirect                                 int
	sfuncSchema, sfunc                           string
	sspace                                       int
	finalSchema, final                           string
	finalExtra                                   bool
	combineSchema, combine                       string
	serialSchema, serial                         string
	deserialSchema, deserial                     string
	initVal                                      sql.NullString
	msfuncSchema, msfunc, mstype                 string
	msspace                                      int
	minit                                        sql.NullString
	minvSchema, minv, mfinalSchema, mfinal       string
	mfinalExtra                                  bool
}

func formatAggregate(a aggregateSpec) string {
	var clauses []string
	addFn := func(label, schema, name string) {
		if name == "" {
			return
		}
		clauses = append(clauses, fmt.Sprintf("%s = %s", label, quoteQualifiedTable(schema, name)))
	}
	addFn("SFUNC", a.sfuncSchema, a.sfunc)
	clauses = append(clauses, "STYPE = "+a.stype)
	if a.sspace > 0 {
		clauses = append(clauses, fmt.Sprintf("SSPACE = %d", a.sspace))
	}
	addFn("FINALFUNC", a.finalSchema, a.final)
	if a.finalExtra {
		clauses = append(clauses, "FINALFUNC_EXTRA")
	}
	addFn("COMBINEFUNC", a.combineSchema, a.combine)
	addFn("SERIALFUNC", a.serialSchema, a.serial)
	addFn("DESERIALFUNC", a.deserialSchema, a.deserial)
	if a.initVal.Valid {
		clauses = append(clauses, "INITCOND = "+quoteLiteral(a.initVal.String))
	}
	addFn("MSFUNC", a.msfuncSchema, a.msfunc)
	if a.mstype != "" {
		clauses = append(clauses, "MSTYPE = "+a.mstype)
	}
	if a.msspace > 0 {
		clauses = append(clauses, fmt.Sprintf("MSSPACE = %d", a.msspace))
	}
	if a.minit.Valid {
		clauses = append(clauses, "MINITCOND = "+quoteLiteral(a.minit.String))
	}
	addFn("MINVFUNC", a.minvSchema, a.minv)
	addFn("MFINALFUNC", a.mfinalSchema, a.mfinal)
	if a.mfinalExtra {
		clauses = append(clauses, "MFINALFUNC_EXTRA")
	}
	switch a.parallel {
	case "s":
		clauses = append(clauses, "PARALLEL = SAFE")
	case "r":
		clauses = append(clauses, "PARALLEL = RESTRICTED")
	default:
		clauses = append(clauses, "PARALLEL = UNSAFE")
	}
	sig := a.args
	if a.aggkind == "o" || a.aggkind == "h" {
		sig = formatOrderedAggregateSignature(a.args, a.aggNumDirect)
	}
	if a.aggkind == "h" {
		clauses = append(clauses, "HYPOTHETICAL")
	}
	return fmt.Sprintf("CREATE AGGREGATE %s(%s) (%s)", quoteQualifiedTable(a.schema, a.name), sig, strings.Join(clauses, ", "))
}

func splitFunctionIdentityArguments(args string) []string {
	if args == "" {
		return nil
	}
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(args[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(args[start:]))
	return out
}

func formatOrderedAggregateSignature(args string, numDirect int) string {
	// pg_get_function_identity_arguments already inserts ORDER BY for
	// ordered-set and hypothetical aggregates. Inserting it again yields
	// "ORDER BY integer ORDER BY integer", which PostgreSQL rejects.
	if strings.Contains(args, " ORDER BY ") || strings.HasPrefix(args, "ORDER BY ") {
		return args
	}
	types := splitFunctionIdentityArguments(args)
	if numDirect <= 0 || numDirect >= len(types) {
		return "ORDER BY " + args
	}
	direct := strings.Join(types[:numDirect], ", ")
	ordered := strings.Join(types[numDirect:], ", ")
	return direct + " ORDER BY " + ordered
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
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_trigger'::regclass
		      AND d.objid = t.oid
		      AND d.deptype = 'e'
		  )
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
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_rewrite'::regclass
		      AND d.objid = r.oid
		      AND d.deptype = 'e'
		  )
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

func applySQLDefs(ctx context.Context, tgt execer, defs []string, label string) error {
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
