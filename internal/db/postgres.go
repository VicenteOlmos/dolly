package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SkipRelationAnnotations skips partition and generated-column lookups.
// Production leaves this false. sqlmock suites set it so existing query
// sequences stay valid; tests that cover the annotations turn it back off.
var SkipRelationAnnotations bool

// LoadPostgresSchemas loads base tables for the given schema names.
// When schemas is nil or empty, only the public schema is loaded.
// Delegates to LoadPostgresSchemasBatched (2 queries for columns + FKs
// regardless of table count, not 2N per-table queries).
func LoadPostgresSchemas(ctx context.Context, q queryer, schemas []string) ([]Table, error) {
	return LoadPostgresSchemasBatched(ctx, q, schemas)
}

// LoadPostgresSchemasBatched loads all table metadata with batched queries
// (3 queries for columns + foreign keys + unique indexes regardless of table count).
// When schemas is nil or empty, only the public schema is loaded.
func LoadPostgresSchemasBatched(ctx context.Context, q queryer, schemas []string) ([]Table, error) {
	filter := schemaFilter(schemas)
	tables, err := fetchTables(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, nil
	}

	colMap, err := fetchAllColumns(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	fkMap, err := fetchAllForeignKeys(ctx, q, filter)
	if err != nil {
		return nil, err
	}
	idxMap, err := fetchUniqueIndexes(ctx, q, filter)
	if err != nil {
		return nil, err
	}

	for i := range tables {
		key := tables[i].Schema + "." + tables[i].Name
		tables[i].Columns = colMap[key]
		tables[i].ForeignKeys = fkMap[key]
		tables[i].UniqueIndexes = idxMap[key]
	}
	if !SkipRelationAnnotations {
		if err := annotateLoadedTables(ctx, q, tables, filter); err != nil {
			return nil, err
		}
	}

	return tables, nil
}

// LoadPostgresPublicSchema loads all public schema tables with columns and foreign keys.
func LoadPostgresPublicSchema(ctx context.Context, q queryer) ([]Table, error) {
	return LoadPostgresSchemas(ctx, q, nil)
}

// ListPostgresSchemaNames returns user-visible schema names for multi-select pickers.
func ListPostgresSchemaNames(ctx context.Context, q queryer) ([]string, error) {
	const query = `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schema_name NOT LIKE 'pg\_temp\_%'
		  AND schema_name NOT LIKE 'pg\_toast\_%'
		ORDER BY schema_name;
	`
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list schemas: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	return names, nil
}

func schemaFilter(schemas []string) []string {
	if len(schemas) == 0 {
		return []string{"public"}
	}
	return schemas
}

func fetchTables(ctx context.Context, q queryer, schemas []string) ([]Table, error) {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	query := fmt.Sprintf(`
		SELECT t.table_schema, t.table_name, s.n_live_tup
		FROM information_schema.tables t
		LEFT JOIN pg_stat_user_tables s
		  ON s.schemaname = t.table_schema AND s.relname = t.table_name
		WHERE t.table_schema IN (%s) AND t.table_type = 'BASE TABLE'
		ORDER BY t.table_schema, t.table_name;
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch tables: %w", err)
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var t Table
		var rowCount sql.NullInt64
		if err := rows.Scan(&t.Schema, &t.Name, &rowCount); err != nil {
			return nil, fmt.Errorf("fetch tables: %w", err)
		}
		if rowCount.Valid {
			t.RowCount = &rowCount.Int64
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch tables: %w", err)
	}

	return tables, nil
}

func fetchAllColumns(ctx context.Context, q queryer, schemas []string) (map[string][]Column, error) {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	// ponytail: replace per-column EXISTS subquery with a LEFT JOIN to the PK
	// constraint/catalog for all tables at once. One query, not N×cols.
	query := fmt.Sprintf(`
		SELECT c.table_schema, c.table_name, c.column_name, c.data_type,
		       c.is_nullable, c.ordinal_position,
		       (kcu.column_name IS NOT NULL) AS is_primary_key
		FROM information_schema.columns c
		LEFT JOIN information_schema.table_constraints tc
		  ON tc.constraint_type = 'PRIMARY KEY'
		  AND tc.table_schema = c.table_schema
		  AND tc.table_name = c.table_name
		LEFT JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_name = tc.constraint_name
		  AND kcu.table_schema = tc.table_schema
		  AND kcu.table_name = tc.table_name
		  AND kcu.column_name = c.column_name
		WHERE c.table_schema IN (%s)
		ORDER BY c.table_schema, c.table_name, c.ordinal_position;
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch columns: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]Column)
	for rows.Next() {
		var schema, table, name, dataType, nullable string
		var ordinal int
		var isPK bool
		if err := rows.Scan(&schema, &table, &name, &dataType, &nullable, &ordinal, &isPK); err != nil {
			return nil, fmt.Errorf("fetch columns: %w", err)
		}
		key := schema + "." + table
		out[key] = append(out[key], Column{
			Name:            name,
			DataType:        dataType,
			IsNullable:      nullable == "YES",
			PrimaryKey:      isPK,
			OrdinalPosition: ordinal,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch columns: %w", err)
	}
	return out, nil
}

func fetchAllForeignKeys(ctx context.Context, q queryer, schemas []string) (map[string][]ForeignKey, error) {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	query := fmt.Sprintf(`
		SELECT tc.table_schema, tc.table_name, tc.constraint_name, kcu.column_name,
		       ccu.table_schema, ccu.table_name, ccu.column_name
		FROM information_schema.table_constraints AS tc
		JOIN information_schema.key_column_usage AS kcu
		  ON tc.constraint_name = kcu.constraint_name
		  AND tc.table_schema = kcu.table_schema
		  AND tc.table_name = kcu.table_name
		JOIN information_schema.referential_constraints AS rc
		  ON tc.constraint_name = rc.constraint_name
		  AND tc.constraint_schema = rc.constraint_schema
		  AND tc.constraint_catalog = rc.constraint_catalog
		JOIN information_schema.constraint_column_usage AS ccu
		  ON rc.unique_constraint_name = ccu.constraint_name
		  AND rc.unique_constraint_schema = ccu.constraint_schema
		  AND rc.unique_constraint_catalog = ccu.constraint_catalog
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema IN (%s)
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name, kcu.column_name;
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch foreign keys: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]ForeignKey)
	for rows.Next() {
		var schema, table string
		var fk ForeignKey
		if err := rows.Scan(&schema, &table, &fk.ConstraintName, &fk.ColumnName,
			&fk.ReferencedTableSchema, &fk.ReferencedTableName, &fk.ReferencedColumnName); err != nil {
			return nil, fmt.Errorf("fetch foreign keys: %w", err)
		}
		key := schema + "." + table
		out[key] = append(out[key], fk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch foreign keys: %w", err)
	}
	return out, nil
}

const (
	maxInt16, minInt16, maxUint32 = int64(32767), int64(-32768), int64(4294967295)
)

// fetchUniqueIndexes loads unique/PK indexes; key cols only (indnkeyatts), INCLUDE excluded.
func fetchUniqueIndexes(ctx context.Context, q queryer, schemas []string) (map[string][]UniqueIndexInfo, error) {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	query := fmt.Sprintf(`
		SELECT
			n.nspname,
			c.relname,
			ic.relname,
			ic.oid,
			i.indisprimary,
			i.indisvalid,
			i.indisready,
			am.amname,
			(i.indpred IS NOT NULL) AS has_predicate,
			(i.indexprs IS NOT NULL) AS is_expression,
			i.indnkeyatts,
			a.attname,
			NOT a.attnotnull,
			col.pos,
			col.attnum,
			opc.opcoid,
			coll.colloid,
			opt.optval
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_am am ON am.oid = ic.relam
		CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS col(attnum, pos)
		CROSS JOIN LATERAL unnest(i.indclass) WITH ORDINALITY AS opc(opcoid, opcpos)
		CROSS JOIN LATERAL unnest(i.indcollation) WITH ORDINALITY AS coll(colloid, collpos)
		CROSS JOIN LATERAL unnest(i.indoption) WITH ORDINALITY AS opt(optval, optpos)
		LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = col.attnum
		WHERE i.indisunique
		  AND col.pos <= i.indnkeyatts
		  AND opc.opcpos = col.pos
		  AND coll.collpos = col.pos
		  AND opt.optpos = col.pos
		  AND n.nspname IN (%s)
		ORDER BY n.nspname, c.relname, ic.relname, col.pos;
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("fetch unique indexes: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]UniqueIndexInfo)
	var currentKey, currentTable string
	var currentIdx *UniqueIndexInfo
	var currentKeyCount, nextOrdinal int

	closeCurrentIndex := func() error {
		if currentIdx == nil {
			return nil
		}
		if nextOrdinal-1 != currentKeyCount {
			return fmt.Errorf("fetch unique indexes: key count mismatch for index %s", currentIdx.IndexName)
		}
		out[currentKey] = append(out[currentKey], *currentIdx)
		currentIdx = nil
		return nil
	}

	for rows.Next() {
		var schema, table, indexName, amName string
		var indexOID int64
		var isPrimary, isValid, isReady, hasPred, isExpr bool
		var keyCount int
		var colName sql.NullString
		var isNullable sql.NullBool
		var pos int
		var attnum, opclassOID, collationOID, indoption int64

		if err := rows.Scan(&schema, &table, &indexName, &indexOID,
			&isPrimary, &isValid, &isReady, &amName,
			&hasPred, &isExpr, &keyCount, &colName, &isNullable, &pos,
			&attnum, &opclassOID, &collationOID, &indoption); err != nil {
			return nil, fmt.Errorf("fetch unique indexes: %w", err)
		}
		if indexOID < 0 || indexOID > maxUint32 {
			return nil, fmt.Errorf("fetch unique indexes: malformed index OID %d", indexOID)
		}
		indexOIDU := uint32(indexOID)

		key := schema + "." + table
		if currentKey != key || currentIdx == nil || currentIdx.IndexName != indexName {
			if err := closeCurrentIndex(); err != nil {
				return nil, err
			}
			currentKey = key
			currentTable = table
			currentKeyCount = keyCount
			nextOrdinal = 1
			currentIdx = &UniqueIndexInfo{
				IndexName:    indexName,
				IndexSchema:  schema,
				IndexOID:     indexOIDU,
				IsPrimary:    isPrimary,
				IsValid:      isValid,
				IsReady:      isReady,
				AccessMethod: amName,
				HasPredicate: hasPred,
				IsExpression: isExpr,
			}
		} else if schema != currentIdx.IndexSchema || table != currentTable ||
			indexOIDU != currentIdx.IndexOID || isPrimary != currentIdx.IsPrimary ||
			isValid != currentIdx.IsValid || isReady != currentIdx.IsReady ||
			amName != currentIdx.AccessMethod || hasPred != currentIdx.HasPredicate ||
			isExpr != currentIdx.IsExpression || keyCount != currentKeyCount {
			return nil, fmt.Errorf("fetch unique indexes: inconsistent metadata for index %s", indexName)
		}

		if pos != nextOrdinal {
			return nil, fmt.Errorf("fetch unique indexes: malformed key position %d for index %s", pos, indexName)
		}
		nextOrdinal++

		if colName.Valid {
			if attnum <= 0 || attnum > maxInt16 {
				return nil, fmt.Errorf("fetch unique indexes: malformed attnum %d for column %s", attnum, colName.String)
			}
			if opclassOID < 0 || opclassOID > maxUint32 || collationOID < 0 || collationOID > maxUint32 {
				return nil, fmt.Errorf("fetch unique indexes: malformed opclass/collation OID for index %s", indexName)
			}
			if indoption < minInt16 || indoption > maxInt16 {
				return nil, fmt.Errorf("fetch unique indexes: malformed indoption %d for index %s", indoption, indexName)
			}
			nullable := !isNullable.Valid || isNullable.Bool
			currentIdx.KeyColumns = append(currentIdx.KeyColumns, UniqueIndexColumn{
				Name:         colName.String,
				Position:     pos,
				IsNullable:   nullable,
				Attnum:       int16(attnum),
				OpclassOID:   uint32(opclassOID),
				CollationOID: uint32(collationOID),
				RawIndoption: int16(indoption),
			})
		} else if attnum != 0 {
			return nil, fmt.Errorf("fetch unique indexes: malformed expression attnum %d", attnum)
		} else {
			currentIdx.IsExpression = true
		}
	}

	if err := closeCurrentIndex(); err != nil {
		return nil, err
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetch unique indexes: %w", err)
	}

	return out, nil
}

func annotateLoadedTables(ctx context.Context, q queryer, tables []Table, schemas []string) error {
	if err := annotatePartitions(ctx, q, tables, schemas); err != nil {
		return err
	}
	return annotateGeneratedColumns(ctx, q, tables, schemas)
}

func annotatePartitions(ctx context.Context, q queryer, tables []Table, schemas []string) error {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	query := fmt.Sprintf(`
		SELECT n.nspname, c.relname, c.relkind::text, c.relispartition,
		       COALESCE(pg_get_partkeydef(c.oid), ''),
		       CASE WHEN c.relispartition THEN COALESCE(pg_get_expr(c.relpartbound, c.oid), '') ELSE '' END,
		       CASE WHEN c.relispartition THEN COALESCE(pn.nspname, '') ELSE '' END,
		       CASE WHEN c.relispartition THEN COALESCE(p.relname, '') ELSE '' END
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_inherits i ON c.relispartition AND i.inhrelid = c.oid
		LEFT JOIN pg_class p ON p.oid = i.inhparent
		LEFT JOIN pg_namespace pn ON pn.oid = p.relnamespace
		WHERE n.nspname IN (%s)
		  AND c.relkind IN ('r', 'p')
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("annotate partitions: %w", err)
	}
	defer rows.Close()

	byName := make(map[string]*Table, len(tables))
	for i := range tables {
		byName[tables[i].Schema+"."+tables[i].Name] = &tables[i]
	}
	for rows.Next() {
		var schema, name, relkind, partBy, bound, parentSchema, parentName string
		var isPartition bool
		if err := rows.Scan(&schema, &name, &relkind, &isPartition, &partBy, &bound, &parentSchema, &parentName); err != nil {
			return fmt.Errorf("annotate partitions: %w", err)
		}
		table := byName[schema+"."+name]
		if table == nil {
			continue
		}
		table.RelKind = relkind
		if relkind == "p" {
			table.PartitionBy = partBy
		}
		if isPartition && parentSchema != "" && parentName != "" {
			table.PartitionOf = parentSchema + "." + parentName
			table.PartitionBound = bound
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("annotate partitions: %w", err)
	}
	return nil
}

func annotateGeneratedColumns(ctx context.Context, q queryer, tables []Table, schemas []string) error {
	placeholders := make([]string, len(schemas))
	args := make([]any, len(schemas))
	for i, schema := range schemas {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = schema
	}
	query := fmt.Sprintf(`
		SELECT table_schema, table_name, column_name
		FROM information_schema.columns
		WHERE is_generated = 'ALWAYS'
		  AND table_schema IN (%s)
	`, strings.Join(placeholders, ", "))
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("annotate generated columns: %w", err)
	}
	defer rows.Close()

	type colKey struct{ schema, table, column string }
	generated := map[colKey]struct{}{}
	for rows.Next() {
		var schema, table, column string
		if err := rows.Scan(&schema, &table, &column); err != nil {
			return fmt.Errorf("annotate generated columns: %w", err)
		}
		generated[colKey{schema, table, column}] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("annotate generated columns: %w", err)
	}
	for i := range tables {
		for j := range tables[i].Columns {
			col := &tables[i].Columns[j]
			if _, ok := generated[colKey{tables[i].Schema, tables[i].Name, col.Name}]; ok {
				col.Generated = true
			}
		}
	}
	return nil
}

// Per-table fetchColumns/fetchForeignKeys removed; LoadPostgresSchemas uses batched queries.
