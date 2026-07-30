package dump

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5"
)

type pkSet struct {
	keys map[string]struct{}
	vals []any
}

type tablePlan struct {
	seedPredicates  []compiledWhere
	pkValues        []any
	compositeFKVals map[string][]any // ponytail: FK col → values for composite-PK child tables; AND'd at export
}

type planResult struct {
	tables       map[string]tablePlan
	rowsExported map[string]int
	tableOrder   []string
}

func planSubset(ctx context.Context, q querier, tables []db.Table, cfg SubsetConfig) (*planResult, error) {
	limits := ApplySubsetLimitDefaults(cfg.Limits)

	if cfg.Percent > 0 {
		if cfg.Percent < 1 || cfg.Percent > 100 {
			return nil, fmt.Errorf("subset: percent must be between 1 and 100, got %d", cfg.Percent)
		}
		if len(cfg.Seeds) > 0 {
			return nil, fmt.Errorf("subset: --percent and --seed-file are mutually exclusive")
		}
	} else {
		if err := ValidateSeeds(cfg.Seeds, tables); err != nil {
			return nil, err
		}
	}

	graph, err := buildFKGraph(tables)
	if err != nil {
		return nil, err
	}

	byName := make(map[string]db.Table, len(tables))
	pkCol := make(map[string]string, len(tables))
	for _, t := range tables {
		byName[tableKey(t.Schema, t.Name)] = t
	}
	resolveTable := func(name string) (string, error) {
		if _, ok := byName[name]; ok {
			return name, nil
		}
		var matches []string
		for key, table := range byName {
			if table.Name == name {
				matches = append(matches, key)
			}
		}
		if len(matches) != 1 {
			return "", fmt.Errorf("seed table %q is ambiguous or not selected", name)
		}
		return matches[0], nil
	}
	pkColumn := func(tableName string) (string, error) {
		if col, ok := pkCol[tableName]; ok {
			return col, nil
		}
		col, err := primaryKeyColumn(byName[tableName])
		if err != nil {
			return "", err
		}
		pkCol[tableName] = col
		return col, nil
	}

	visited := make(map[string]*pkSet)
	compositeWhere := make(map[string]map[string][]any) // ponytail: FK col → values for composite-PK child tables
	requiredParents := make(map[string]map[string]struct{})
	rowBudget := 0

	ensureTable := func(name string, currentDepth int) error {
		if len(visited) >= limits.MaxTables {
			if _, ok := visited[name]; !ok {
				return newLimitError("max_tables", "would include table %q but MaxTables=%d", name, limits.MaxTables)
			}
		}
		if currentDepth > limits.MaxDepth {
			return newLimitError("max_depth", "exceeded MaxDepth=%d at table %q", limits.MaxDepth, name)
		}
		return nil
	}

	addPK := func(table string, pk any, depth int) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := ensureTable(table, depth); err != nil {
			return false, err
		}
		key := pkKey(pk)
		set := visited[table]
		if set == nil {
			set = &pkSet{keys: make(map[string]struct{})}
			visited[table] = set
		}
		if _, ok := set.keys[key]; ok {
			return false, nil
		}
		set.keys[key] = struct{}{}
		set.vals = append(set.vals, pk)
		return true, nil
	}

	countRows := func(n int) error {
		rowBudget += n
		if rowBudget > limits.MaxRows {
			return newLimitError("max_rows", "exceeded MaxRows=%d", limits.MaxRows)
		}
		return nil
	}

	seedByTable := make(map[string][]compiledWhere)

	if cfg.Percent > 0 {
		roots, err := selectPercentRoots(ctx, q, tables, cfg.Percent)
		if err != nil {
			return nil, fmt.Errorf("percent subset: %w", err)
		}
		for _, root := range roots {
			rootKey := tableKey(root.table.Schema, root.table.Name)
			pk, err := pkColumn(rootKey)
			if err != nil {
				return nil, fmt.Errorf("percent table %q: %w", root.table.Name, err)
			}
			// Clamp limit to remaining MaxRows budget before querying.
			remainingBudget := limits.MaxRows - rowBudget
			limit := root.limit
			if limits.MaxRowsPerTable > 0 && limit > limits.MaxRowsPerTable {
				limit = limits.MaxRowsPerTable
			}
			if limit > remainingBudget {
				if remainingBudget <= 0 {
					return nil, newLimitError("max_rows", "exceeded MaxRows=%d", limits.MaxRows)
				}
				limit = remainingBudget
			}
			pks, err := selectRecentPKs(ctx, q, root.table, root.pkCol, root.orderCol, limit)
			if err != nil {
				return nil, fmt.Errorf("percent table %q: %w", root.table.Name, err)
			}
			if err := countRows(len(pks)); err != nil {
				return nil, err
			}
			for _, val := range pks {
				if _, err := addPK(tableKey(root.table.Schema, root.table.Name), val, 0); err != nil {
					return nil, err
				}
			}
			_ = pk // pk is same as root.pkCol (validated above)
		}
		if err := expandPercentClosure(ctx, q, byName, graph, visited, compositeWhere, requiredParents, limits, cfg.Percent, pkColumn, addPK, countRows, ensureTable, func() int {
			return limits.MaxRows - rowBudget
		}); err != nil {
			return nil, err
		}
	} else {
		for _, raw := range cfg.Seeds {
			p := normalizePredicate(raw)
			tableKey, err := resolveTable(p.Table)
			if err != nil {
				return nil, err
			}
			cw, err := compilePredicate(p)
			if err != nil {
				return nil, err
			}
			seedByTable[tableKey] = append(seedByTable[tableKey], cw)

			tbl := byName[tableKey]
			pk, err := pkColumn(tableKey)
			if err != nil {
				return nil, fmt.Errorf("seed table %q: %w", p.Table, err)
			}
			pks, err := selectPKs(ctx, q, tbl, pk, []compiledWhere{cw})
			if err != nil {
				return nil, fmt.Errorf("seed table %q: %w", p.Table, err)
			}
			if err := countRows(len(pks)); err != nil {
				return nil, err
			}
			for _, pk := range pks {
				if _, err := addPK(tableKey, pk, 0); err != nil {
					return nil, err
				}
			}
		}

		type queueItem struct {
			table string
			pk    any
			depth int
		}

		var queue []queueItem
		// Deterministic queue initialization: sort visited tables by key before
		// adding their PKs. Without this, Go map iteration randomness causes
		// different FK closure traversal order across repeated runs.
		visitedKeys := make([]string, 0, len(visited))
		for k := range visited {
			visitedKeys = append(visitedKeys, k)
		}
		sort.Strings(visitedKeys)
		for _, table := range visitedKeys {
			for _, pk := range visited[table].vals {
				queue = append(queue, queueItem{table: table, pk: pk, depth: 0})
			}
		}

		tableSlots := make(map[string]int)
		if limits.MaxRowsPerTable > 0 {
			for table, set := range visited {
				tableSlots[table] = limits.MaxRowsPerTable - len(set.vals)
			}
		}

		for len(queue) > 0 {
			item := queue[0]
			queue = queue[1:]

			if err := ctx.Err(); err != nil {
				return nil, err
			}

			tbl := byName[item.table]
			nextDepth := item.depth + 1

			for _, edge := range graph.childToParents[item.table] {
				childPK, err := pkColumn(item.table)
				if err != nil {
					return nil, err
				}
				parentVals, err := selectFKTargets(ctx, q, tbl, edge.childColumn, childPK, item.pk)
				if err != nil {
					return nil, fmt.Errorf("parent hop %q -> %q: %w", item.table, edge.parentTable, err)
				}
				if err := countRows(len(parentVals)); err != nil {
					return nil, err
				}
				for _, pv := range parentVals {
					if pv == nil {
						continue
					}
					if requiredParents[item.table] == nil {
						requiredParents[item.table] = make(map[string]struct{})
					}
					requiredParents[item.table][edge.parentTable] = struct{}{}
					newPK, err := addPK(edge.parentTable, pv, nextDepth)
					if err != nil {
						return nil, err
					}
					if newPK {
						queue = append(queue, queueItem{table: edge.parentTable, pk: pv, depth: nextDepth})
					}
				}
			}

			for _, edge := range graph.parentToChildren[item.table] {
				childPKName, err := pkColumn(edge.childTable)
				if err != nil {
					if err := ensureTable(edge.childTable, nextDepth); err != nil {
						return nil, err
					}
					if visited[edge.childTable] == nil {
						visited[edge.childTable] = &pkSet{keys: make(map[string]struct{})}
					}
					if compositeWhere[edge.childTable] == nil {
						compositeWhere[edge.childTable] = make(map[string][]any)
					}
					fkColVals := compositeWhere[edge.childTable][edge.childColumn]
					compositeWhere[edge.childTable][edge.childColumn] = append(fkColVals, item.pk)
					continue
				}
				remaining := limits.MaxRows - rowBudget
				tableRemaining := remaining
				if limits.MaxRowsPerTable > 0 {
					if tableSlots[edge.childTable] <= 0 {
						continue
					}
					if tableRemaining > tableSlots[edge.childTable] {
						tableRemaining = tableSlots[edge.childTable]
					}
				}
				orderCol, _ := selectOrderColumn(byName[edge.childTable])
				childPKs, err := selectChildPKs(ctx, q, byName[edge.childTable], childPKName, edge.childColumn, item.pk, orderCol, tableRemaining, limits.MaxRowsPerTable > 0)
				if err != nil {
					return nil, fmt.Errorf("child hop %q -> %q: %w", item.table, edge.childTable, err)
				}
				if err := countRows(len(childPKs)); err != nil {
					return nil, err
				}
				for _, cpk := range childPKs {
					newPK, err := addPK(edge.childTable, cpk, nextDepth)
					if err != nil {
						return nil, err
					}
					if newPK {
						if limits.MaxRowsPerTable > 0 {
							tableSlots[edge.childTable]--
						}
						queue = append(queue, queueItem{table: edge.childTable, pk: cpk, depth: nextDepth})
					}
				}
			}
		}
	}

	// Post-BFS fixup: for each composite-PK table in visited, ensure ALL FK
	// columns whose parent is in visited are represented in compositeWhere.
	// If a parent has 0 PKs, the empty list makes the AND predicate return
	// false (0 rows) — correct, because the child can't reference non-existent
	// parent rows. Without this, a join table with FK to a 0-row parent would
	// export orphan rows that violate FK on restore.
	for _, t := range tables {
		key := tableKey(t.Schema, t.Name)
		if _, ok := visited[key]; !ok {
			continue
		}
		// Only composite-PK tables use compositeWhere.
		if _, err := pkColumn(key); err == nil {
			continue
		}
		for _, fk := range t.ForeignKeys {
			parentSet, ok := visited[tableKey(fk.ReferencedTableSchema, fk.ReferencedTableName)]
			if !ok {
				continue
			}
			if compositeWhere[key] == nil {
				compositeWhere[key] = make(map[string][]any)
			}
			if _, exists := compositeWhere[key][fk.ColumnName]; exists {
				continue
			}
			// Parent is visited but this FK column wasn't populated during BFS.
			// Add parent PKs (may be empty → AND predicate will be false → 0 rows).
			if parentSet != nil {
				compositeWhere[key][fk.ColumnName] = append([]any{}, parentSet.vals...)
			}
		}
	}

	if err := verifyClosureIntegrity(visited, requiredParents); err != nil {
		return nil, err
	}

	plans := make(map[string]tablePlan, len(visited))
	rowsExported := make(map[string]int, len(visited))
	var included []db.Table
	for _, t := range tables {
		key := tableKey(t.Schema, t.Name)
		set, ok := visited[key]
		if !ok {
			continue
		}
		included = append(included, t)
		plans[key] = tablePlan{
			seedPredicates:  seedByTable[key],
			pkValues:        set.vals,
			compositeFKVals: compositeWhere[key],
		}
		if len(set.vals) > 0 {
			rowsExported[key] = len(set.vals)
		} else if fkVals := compositeWhere[key]; len(fkVals) > 0 {
			total := 0
			for _, vals := range fkVals {
				total += len(vals)
			}
			rowsExported[key] = total // ponytail: sum of FK values across all columns
		}
	}

	sorted := SortTables(included)
	names := make([]string, len(sorted))
	for i, t := range sorted {
		names[i] = tableKey(t.Schema, t.Name)
	}

	return &planResult{
		tables:       plans,
		rowsExported: rowsExported,
		tableOrder:   names,
	}, nil
}

func verifyClosureIntegrity(visited map[string]*pkSet, requiredParents map[string]map[string]struct{}) error {
	for childTable, parents := range requiredParents {
		for parentTable := range parents {
			if _, ok := visited[parentTable]; !ok {
				return fmt.Errorf("subset closure integrity: table %q references parent %q which is not in the export plan", childTable, parentTable)
			}
		}
	}
	return nil
}

func selectPKs(ctx context.Context, q querier, table db.Table, pkCol string, wheres []compiledWhere) ([]any, error) {
	whereSQL, args, err := mergeWhereClauses(wheres, 1)
	if err != nil {
		return nil, err
	}
	pkIdent := pgx.Identifier{pkCol}.Sanitize()
	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s", pkIdent, tableIdent, whereSQL)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []any
	for rows.Next() {
		var pk any
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

func selectFKTargets(ctx context.Context, q querier, table db.Table, fkCol, pkCol string, pk any) ([]any, error) {
	fkIdent := pgx.Identifier{fkCol}.Sanitize()
	pkIdent := pgx.Identifier{pkCol}.Sanitize()
	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", fkIdent, tableIdent, pkIdent)
	rows, err := q.QueryContext(ctx, query, pk)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func selectChildPKs(ctx context.Context, q querier, child db.Table, childPK, fkCol string, parentPK any, orderCol string, maxRows int, recencyLimit bool) ([]any, error) {
	childPKIdent := pgx.Identifier{childPK}.Sanitize()
	fkIdent := pgx.Identifier{fkCol}.Sanitize()
	tableIdent := pgx.Identifier{child.Schema, child.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", childPKIdent, tableIdent, fkIdent)
	if recencyLimit && orderCol != "" {
		orderIdent := pgx.Identifier{orderCol}.Sanitize()
		query += fmt.Sprintf(" ORDER BY %s DESC NULLS LAST", orderIdent)
		if orderCol != childPK {
			query += fmt.Sprintf(", %s DESC", childPKIdent)
		}
	}
	if maxRows < 0 {
		maxRows = 0
	}
	// LIMIT maxRows+1 detects overflow without fetching all child rows.
	query += fmt.Sprintf(" LIMIT %d", maxRows+1)
	rows, err := q.QueryContext(ctx, query, parentPK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []any
	for rows.Next() {
		var pk any
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !recencyLimit && len(out) > maxRows {
		return nil, newLimitError("max_rows", "child closure for %q would exceed MaxRows", child.Name)
	}
	if recencyLimit && len(out) > maxRows {
		out = out[:maxRows]
	}
	return out, nil
}

func mergeWhereClauses(clauses []compiledWhere, startArg int) (string, []any, error) {
	if len(clauses) == 0 {
		return "FALSE", nil, nil
	}
	var parts []string
	var args []any
	argN := startArg
	for _, c := range clauses {
		rewritten, next, err := renumberWhere(c.sql, c.args, argN)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, rewritten)
		args = append(args, c.args...)
		argN = next
	}
	return strings.Join(parts, " OR "), args, nil
}

func renumberWhere(sql string, args []any, start int) (string, int, error) {
	if len(args) == 0 {
		return sql, start, nil
	}
	next := start + len(args)
	out := sql
	for i := len(args); i >= 1; i-- {
		old := fmt.Sprintf("$%d", i)
		new := fmt.Sprintf("$%d", start+i-1)
		out = strings.Replace(out, old, new, 1)
	}
	return out, next, nil
}

func pkKey(v any) string {
	return fmt.Sprintf("%T:%v", v, v)
}

func buildStreamClauses(pkCol string, plan tablePlan, limits SubsetLimits) ([]compiledWhere, error) {
	if len(plan.pkValues) > 0 {
		return expandPKChunks(pkCol, plan.pkValues, limits.MaxInListSize), nil
	}
	if len(plan.compositeFKVals) > 0 {
		return buildCompositeFKPredicate(plan.compositeFKVals), nil
	}
	if len(plan.seedPredicates) > 0 {
		return plan.seedPredicates, nil
	}
	// Table is in the closure but has 0 selected rows (e.g. a child table
	// whose parent had no in-scope rows). Export an empty NDJSON file.
	return []compiledWhere{{sql: "false", args: nil}}, nil
}

// buildCompositeFKPredicate builds a single AND'd compiledWhere from FK-column
// value groups. Each FK column gets an IN clause; all IN clauses are AND'd
// so join tables with multiple FKs only export rows where ALL parent PKs exist
// in the selected set.
func buildCompositeFKPredicate(fkGrouped map[string][]any) []compiledWhere {
	cols := make([]string, 0, len(fkGrouped))
	for col := range fkGrouped {
		cols = append(cols, col)
	}
	sort.Strings(cols)

	var parts []string
	var args []any
	for i, col := range cols {
		ident := pgx.Identifier{col}.Sanitize()
		parts = append(parts, fmt.Sprintf("(%s = ANY($%d))", ident, i+1))
		args = append(args, toDriverArrayArg(fkGrouped[col]))
	}
	return []compiledWhere{{
		sql:  strings.Join(parts, " AND "),
		args: args,
	}}
}

func expandPKChunks(pkCol string, values []any, maxSize int) []compiledWhere {
	if maxSize <= 0 || len(values) <= maxSize {
		return []compiledWhere{compilePKInList(pkCol, values)}
	}
	var out []compiledWhere
	for i := 0; i < len(values); i += maxSize {
		end := i + maxSize
		if end > len(values) {
			end = len(values)
		}
		out = append(out, compilePKInList(pkCol, values[i:end]))
	}
	return out
}

// orderColumnPriority lists column names (case-insensitive) in desc priority
// for percent-subset recency ordering.
var orderColumnPriority = []string{
	"created_at", "creation_date", "createdat", "created_on",
	"createdon", "date", "timestamp", "updated_at", "updatedat",
}

// selectOrderColumn returns the best recency column for a table.
// Returns (columnName, true) if a recency column or integer PK fallback exists.
func selectOrderColumn(table db.Table) (string, bool) {
	// ponytail: case-insensitive lookup — most DBs use snake_case but not guaranteed.
	lowerToOrig := make(map[string]string, len(table.Columns))
	for _, c := range table.Columns {
		lowerToOrig[strings.ToLower(c.Name)] = c.Name
	}
	for _, name := range orderColumnPriority {
		if orig, ok := lowerToOrig[name]; ok {
			return orig, true
		}
	}
	// Fallback: integer/numeric single-column PK DESC.
	pk, err := primaryKeyColumn(table)
	if err != nil {
		return "", false
	}
	for _, c := range table.Columns {
		if c.Name != pk {
			continue
		}
		dt := strings.ToLower(c.DataType)
		switch dt {
		case "integer", "bigint", "smallint", "int", "int2", "int4", "int8",
			"serial", "bigserial", "smallserial",
			"numeric", "decimal", "real", "double precision":
			return pk, true
		}
	}
	return "", false
}

// isPureJoinTable returns true when table is a pure many-to-many junction:
// composite PK where every PK column is also an FK column, and the table has
// no recency column.
func isPureJoinTable(table db.Table) bool {
	var pkCols []string
	for _, c := range table.Columns {
		if c.PrimaryKey {
			pkCols = append(pkCols, c.Name)
		}
	}
	if len(pkCols) <= 1 {
		return false
	}
	// Check if table has a recency column.
	for _, c := range table.Columns {
		for _, name := range orderColumnPriority {
			if strings.EqualFold(c.Name, name) {
				return false // has recency column, keep it
			}
		}
	}
	// Check if all PK columns are FK columns.
	fkCols := make(map[string]bool, len(table.ForeignKeys))
	for _, fk := range table.ForeignKeys {
		fkCols[fk.ColumnName] = true
	}
	for _, pk := range pkCols {
		if !fkCols[pk] {
			return false
		}
	}
	return true
}

type percentRoot struct {
	table    db.Table
	pkCol    string
	orderCol string
	limit    int
}

// selectPercentRoots identifies tables suitable as percent-subset roots and
// returns them with their precomputed row limits.
func selectPercentRoots(ctx context.Context, q querier, tables []db.Table, percent int) ([]percentRoot, error) {
	tableNames := make(map[string]struct{}, len(tables))
	for _, t := range tables {
		tableNames[t.Name] = struct{}{}
	}

	var roots []percentRoot
	for _, t := range tables {
		pkCol, err := primaryKeyColumn(t)
		if err != nil {
			continue // skip composite-PK or no-PK tables
		}
		if isPureJoinTable(t) {
			continue
		}
		if !isPercentRootTable(t, tableNames) {
			continue
		}
		orderCol, ok := selectOrderColumn(t)
		if !ok {
			continue
		}

		// Use metadata estimate when available to avoid COUNT(*) on large tables.
		var count int
		if t.RowCount != nil {
			count = int(*t.RowCount)
		} else {
			tableIdent := pgx.Identifier{t.Schema, t.Name}.Sanitize()
			rows, err := q.QueryContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableIdent))
			if err != nil {
				return nil, fmt.Errorf("count rows for table %q: %w", t.Name, err)
			}
			if rows.Next() {
				if err := rows.Scan(&count); err != nil {
					rows.Close()
					return nil, fmt.Errorf("count rows for table %q: %w", t.Name, err)
				}
			}
			rows.Close()
		}
		if count == 0 {
			continue
		}

		limit := percentRowLimit(count, percent)

		roots = append(roots, percentRoot{
			table:    t,
			pkCol:    pkCol,
			orderCol: orderCol,
			limit:    limit,
		})
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("percent subset: no candidate root tables found with recency columns or integer PKs")
	}
	return roots, nil
}

func isPercentRootTable(t db.Table, tableNames map[string]struct{}) bool {
	for _, fk := range t.ForeignKeys {
		if _, ok := tableNames[fk.ReferencedTableName]; ok {
			return false
		}
	}
	return true
}

// selectRecentPKs returns the most recent PK values for a table.
func selectRecentPKs(ctx context.Context, q querier, table db.Table, pkCol, orderCol string, limit int) ([]any, error) {
	pkIdent := pgx.Identifier{pkCol}.Sanitize()
	orderIdent := pgx.Identifier{orderCol}.Sanitize()
	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s DESC NULLS LAST", pkIdent, tableIdent, orderIdent)
	if orderCol != pkCol {
		// ponytail: tie-breaker for deterministic ordering when recency column has duplicates.
		query += fmt.Sprintf(", %s DESC", pkIdent)
	}
	query += fmt.Sprintf(" LIMIT %d", limit)
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []any
	for rows.Next() {
		var pk any
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	return out, rows.Err()
}

func percentRowLimit(count, percent int) int {
	if count <= 0 {
		return 0
	}
	limit := int(math.Ceil(float64(count) * float64(percent) / 100.0))
	if limit < 1 {
		limit = 1
	}
	return limit
}

func resolveSampleLimit(count, percent, remainingBudget int) (int, error) {
	limit := percentRowLimit(count, percent)
	if remainingBudget <= 0 {
		return 0, newLimitError("max_rows", "exceeded MaxRows budget")
	}
	if limit > remainingBudget {
		limit = remainingBudget
	}
	return limit, nil
}

func inScopeFKWhere(fkCol string, parentPKs []any, startArg int) (string, []any) {
	fkIdent := pgx.Identifier{fkCol}.Sanitize()
	parts := make([]string, len(parentPKs))
	args := make([]any, len(parentPKs))
	for i, pk := range parentPKs {
		parts[i] = fmt.Sprintf("$%d", startArg+i)
		args[i] = pk
	}
	return fmt.Sprintf("%s IN (%s)", fkIdent, strings.Join(parts, ",")), args
}

func countRowsInScope(ctx context.Context, q querier, table db.Table, fkCol string, parentPKs []any) (int, error) {
	if len(parentPKs) == 0 {
		return 0, nil
	}
	where, args := inScopeFKWhere(fkCol, parentPKs, 1)
	tableIdent := pgx.Identifier{table.Schema, table.Name}.Sanitize()
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", tableIdent, where)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, err
		}
	}
	return count, rows.Err()
}

func selectRecentChildPKsInScope(ctx context.Context, q querier, child db.Table, childPK, fkCol, orderCol string, parentPKs []any, limit int, overflowCheck bool) ([]any, error) {
	if len(parentPKs) == 0 || limit <= 0 {
		return nil, nil
	}
	where, args := inScopeFKWhere(fkCol, parentPKs, 1)
	childPKIdent := pgx.Identifier{childPK}.Sanitize()
	orderIdent := pgx.Identifier{orderCol}.Sanitize()
	tableIdent := pgx.Identifier{child.Schema, child.Name}.Sanitize()
	queryLimit := limit
	if overflowCheck {
		queryLimit = limit + 1
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s ORDER BY %s DESC NULLS LAST",
		childPKIdent, tableIdent, where, orderIdent,
	)
	if orderCol != childPK {
		query += fmt.Sprintf(", %s DESC", childPKIdent)
	}
	query += fmt.Sprintf(" LIMIT %d", queryLimit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []any
	for rows.Next() {
		var pk any
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		out = append(out, pk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if overflowCheck && len(out) > limit {
		return nil, newLimitError("max_rows", "child closure for %q would exceed MaxRows", child.Name)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func expandPercentClosure(
	ctx context.Context,
	q querier,
	byName map[string]db.Table,
	graph *fkGraph,
	visited map[string]*pkSet,
	compositeWhere map[string]map[string][]any,
	requiredParents map[string]map[string]struct{},
	limits SubsetLimits,
	percent int,
	pkColumn func(string) (string, error),
	addPK func(string, any, int) (bool, error),
	countRows func(int) error,
	ensureTable func(string, int) error,
	remainingBudget func() int,
) error {
	levelTables := make(map[string]struct{})
	for table := range visited {
		levelTables[table] = struct{}{}
	}

	// Deterministic level iteration: collect and sort level table keys
	// so that parent-to-child edge exploration is repeatable.
	getSortedLevelKeys := func(m map[string]struct{}) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}

	for depth := 0; depth < limits.MaxDepth && len(levelTables) > 0; depth++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		expandParents := make(map[string]struct{})
		for t := range levelTables {
			expandParents[t] = struct{}{}
		}

		addedThisLevel := make(map[string]struct{})
		childSampled := make(map[string]struct{})

		for _, table := range getSortedLevelKeys(levelTables) {
			tbl := byName[table]
			childPK, err := pkColumn(table)
			if err != nil {
				continue
			}
			for _, pk := range visited[table].vals {
				for _, edge := range graph.childToParents[table] {
					parentVals, err := selectFKTargets(ctx, q, tbl, edge.childColumn, childPK, pk)
					if err != nil {
						return fmt.Errorf("parent hop %q -> %q: %w", table, edge.parentTable, err)
					}
					if err := countRows(len(parentVals)); err != nil {
						return err
					}
					for _, pv := range parentVals {
						if pv == nil {
							continue
						}
						if requiredParents[table] == nil {
							requiredParents[table] = make(map[string]struct{})
						}
						requiredParents[table][edge.parentTable] = struct{}{}
						newPK, err := addPK(edge.parentTable, pv, depth+1)
						if err != nil {
							return err
						}
						if newPK {
							expandParents[edge.parentTable] = struct{}{}
							addedThisLevel[edge.parentTable] = struct{}{}
						}
					}
				}

				for _, edge := range graph.parentToChildren[table] {
					if _, err := pkColumn(edge.childTable); err != nil {
						if err := ensureTable(edge.childTable, depth+1); err != nil {
							return err
						}
						if visited[edge.childTable] == nil {
							visited[edge.childTable] = &pkSet{keys: make(map[string]struct{})}
						}
						if compositeWhere[edge.childTable] == nil {
							compositeWhere[edge.childTable] = make(map[string][]any)
						}
						compositeWhere[edge.childTable][edge.childColumn] = append(
							compositeWhere[edge.childTable][edge.childColumn], pk)
					}
				}
			}
		}

		// Deterministic parent traversal: sort expandParents keys so that
		// child-per-table sampling order is repeatable across runs.
		expandKeys := make([]string, 0, len(expandParents))
		for k := range expandParents {
			expandKeys = append(expandKeys, k)
		}
		sort.Strings(expandKeys)
		for _, parentTable := range expandKeys {
			parentPKs := visited[parentTable].vals
			if len(parentPKs) == 0 {
				continue
			}
			for _, edge := range graph.parentToChildren[parentTable] {
				childPKName, err := pkColumn(edge.childTable)
				if err != nil {
					continue
				}
				if _, ok := childSampled[edge.childTable]; ok {
					continue
				}
				childSampled[edge.childTable] = struct{}{}

				if err := ensureTable(edge.childTable, depth+1); err != nil {
					return err
				}

				child := byName[edge.childTable]
				orderCol, ok := selectOrderColumn(child)
				if !ok {
					continue
				}

				existing := 0
				if set := visited[edge.childTable]; set != nil {
					existing = len(set.vals)
				}
				tableRemaining := -1
				if limits.MaxRowsPerTable > 0 {
					tableRemaining = limits.MaxRowsPerTable - existing
					if tableRemaining <= 0 {
						continue
					}
				}

				inScope, err := countRowsInScope(ctx, q, child, edge.childColumn, parentPKs)
				if err != nil {
					return fmt.Errorf("count child rows %q: %w", edge.childTable, err)
				}
				if inScope == 0 {
					// Still register the table in visited with 0 PKs so that
					// composite-PK children with FKs to this table AND an empty
					// IN clause (0 rows) instead of skipping the FK entirely.
					if err := ensureTable(edge.childTable, depth+1); err != nil {
						return err
					}
					if visited[edge.childTable] == nil {
						visited[edge.childTable] = &pkSet{keys: make(map[string]struct{})}
					}
					continue
				}

				budget := remainingBudget()
				limit, err := resolveSampleLimit(inScope, percent, budget)
				if err != nil {
					return err
				}
				if tableRemaining > 0 && limit > tableRemaining {
					limit = tableRemaining
				}
				if limit <= 0 {
					continue
				}

				overflowCheck := limits.MaxRowsPerTable <= 0
				childPKs, err := selectRecentChildPKsInScope(ctx, q, child, childPKName, edge.childColumn, orderCol, parentPKs, limit, overflowCheck)
				if err != nil {
					return fmt.Errorf("child hop %q -> %q: %w", parentTable, edge.childTable, err)
				}
				if err := countRows(len(childPKs)); err != nil {
					return err
				}
				for _, cpk := range childPKs {
					newPK, err := addPK(edge.childTable, cpk, depth+1)
					if err != nil {
						return err
					}
					if newPK {
						addedThisLevel[edge.childTable] = struct{}{}
					}
				}
			}
		}

		levelTables = addedThisLevel
	}
	return nil
}
