package clone

import (
	"context"
	"database/sql"
	"fmt"
)

type operatorFamilyDef struct {
	schema string
	name   string
	method string
}

type operatorClassOperator struct {
	strategy      int
	opSchema      string
	opName        string
	leftType      string
	rightType     string
	purpose       string
	sortFamSchema string
	sortFamName   string
}

type operatorClassFunction struct {
	supportNum int
	fnSchema   string
	fnName     string
	fnArgs     string
}

type operatorClassDef struct {
	schema       string
	name         string
	defaultClass bool
	inputType    string
	method       string
	familySchema string
	familyName   string
	storageType  string
	operators    []operatorClassOperator
	functions    []operatorClassFunction
}

const sqlCatalogOrQualifiedType = `
CASE WHEN %s.nspname = 'pg_catalog' THEN pg_catalog.format_type(%s, NULL)
     ELSE quote_ident(%s.nspname) || '.' || quote_ident(%s.typname) END`

func loadOperatorFamilies(ctx context.Context, q *sql.DB, schemas []string) ([]operatorFamilyDef, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT n.nspname, f.opfname, am.amname
		FROM pg_opfamily f
		INNER JOIN pg_namespace n ON n.oid = f.opfnamespace
		INNER JOIN pg_am am ON am.oid = f.opfmethod
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_opfamily'::regclass
		      AND d.objid = f.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, f.opfname`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list operator families: %w", err)
	}
	defer rows.Close()

	var out []operatorFamilyDef
	for rows.Next() {
		var f operatorFamilyDef
		if err := rows.Scan(&f.schema, &f.name, &f.method); err != nil {
			return nil, fmt.Errorf("scan operator family: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func loadOperatorClasses(ctx context.Context, q *sql.DB, schemas []string) ([]operatorClassDef, []string, error) {
	inClause, args := schemaINClause(schemas)
	inTypeSQL := fmt.Sprintf(sqlCatalogOrQualifiedType, "in_ns", "opc.opcintype", "in_ns", "in_t")
	keyTypeSQL := fmt.Sprintf(`
CASE WHEN opc.opckeytype = 0 THEN ''
     ELSE `+sqlCatalogOrQualifiedType+` END`, "key_ns", "opc.opckeytype", "key_ns", "key_t")
	query := fmt.Sprintf(`
		SELECT opc.oid,
		       n.nspname, opc.opcname, opc.opcdefault,
		       %s,
		       am.amname,
		       fam_ns.nspname, fam.opfname,
		       %s
		FROM pg_opclass opc
		INNER JOIN pg_namespace n ON n.oid = opc.opcnamespace
		INNER JOIN pg_type in_t ON in_t.oid = opc.opcintype
		INNER JOIN pg_namespace in_ns ON in_ns.oid = in_t.typnamespace
		INNER JOIN pg_am am ON am.oid = opc.opcmethod
		INNER JOIN pg_opfamily fam ON fam.oid = opc.opcfamily
		INNER JOIN pg_namespace fam_ns ON fam_ns.oid = fam.opfnamespace
		LEFT JOIN pg_type key_t ON key_t.oid = opc.opckeytype AND opc.opckeytype <> 0
		LEFT JOIN pg_namespace key_ns ON key_ns.oid = key_t.typnamespace
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_opclass'::regclass
		      AND d.objid = opc.oid
		      AND d.deptype = 'e'
		  )
		ORDER BY n.nspname, opc.opcname`, inTypeSQL, keyTypeSQL, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list operator classes: %w", err)
	}
	defer rows.Close()

	var out []operatorClassDef
	var oids []uint32
	byOID := make(map[uint32]*operatorClassDef)
	for rows.Next() {
		var oid uint32
		var c operatorClassDef
		if err := rows.Scan(
			&oid,
			&c.schema, &c.name, &c.defaultClass,
			&c.inputType,
			&c.method,
			&c.familySchema, &c.familyName,
			&c.storageType,
		); err != nil {
			return nil, nil, fmt.Errorf("scan operator class: %w", err)
		}
		byOID[oid] = &c
		oids = append(oids, oid)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list operator classes: %w", err)
	}

	if err := mergeOperatorClassOperators(ctx, q, schemas, byOID); err != nil {
		return nil, nil, err
	}
	if err := mergeOperatorClassFunctions(ctx, q, schemas, byOID); err != nil {
		return nil, nil, err
	}
	loose, err := loadLooseOperatorFamilyMembers(ctx, q, schemas)
	if err != nil {
		return nil, nil, err
	}
	if len(oids) == 0 {
		return nil, loose, nil
	}

	for _, oid := range oids {
		c := byOID[oid]
		if len(c.operators) == 0 && len(c.functions) == 0 {
			continue
		}
		out = append(out, *c)
	}
	return out, loose, nil
}

func mergeOperatorClassOperators(ctx context.Context, q *sql.DB, schemas []string, byOID map[uint32]*operatorClassDef) error {
	inClause, args := schemaINClause(schemas)
	leftTypeSQL := fmt.Sprintf(sqlCatalogOrQualifiedType, "lt_ns", "amop.amoplefttype", "lt_ns", "lt_t")
	rightTypeSQL := fmt.Sprintf(sqlCatalogOrQualifiedType, "rt_ns", "amop.amoprighttype", "rt_ns", "rt_t")
	query := fmt.Sprintf(`
		SELECT opc.oid,
		       amop.amopstrategy,
		       opn.nspname, o.oprname,
		       %s,
		       %s,
		       amop.amoppurpose::text,
		       COALESCE(sort_ns.nspname, ''),
		       COALESCE(sort_f.opfname, '')
		FROM pg_opclass opc
		INNER JOIN pg_namespace n ON n.oid = opc.opcnamespace
		INNER JOIN pg_amop amop ON amop.amopfamily = opc.opcfamily
		  AND amop.amoplefttype = opc.opcintype
		  AND amop.amoprighttype = opc.opcintype
		INNER JOIN pg_operator o ON o.oid = amop.amopopr
		INNER JOIN pg_namespace opn ON opn.oid = o.oprnamespace
		INNER JOIN pg_type lt_t ON lt_t.oid = amop.amoplefttype
		INNER JOIN pg_namespace lt_ns ON lt_ns.oid = lt_t.typnamespace
		INNER JOIN pg_type rt_t ON rt_t.oid = amop.amoprighttype
		INNER JOIN pg_namespace rt_ns ON rt_ns.oid = rt_t.typnamespace
		LEFT JOIN pg_opfamily sort_f ON sort_f.oid = amop.amopsortfamily
		LEFT JOIN pg_namespace sort_ns ON sort_ns.oid = sort_f.opfnamespace
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_opclass'::regclass
		      AND d.objid = opc.oid
		      AND d.deptype = 'e'
		  )
		  AND EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_amop'::regclass
		      AND d.objid = amop.oid
		      AND d.refclassid = 'pg_opclass'::regclass
		      AND d.refobjid = opc.oid
		      AND d.deptype = 'i'
		  )
		ORDER BY opc.oid, amop.amopstrategy`, leftTypeSQL, rightTypeSQL, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list operator class operators: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var oid uint32
		var op operatorClassOperator
		if err := rows.Scan(
			&oid,
			&op.strategy,
			&op.opSchema, &op.opName,
			&op.leftType, &op.rightType,
			&op.purpose,
			&op.sortFamSchema, &op.sortFamName,
		); err != nil {
			return fmt.Errorf("scan operator class operator: %w", err)
		}
		c := byOID[oid]
		if c == nil {
			continue
		}
		c.operators = append(c.operators, op)
	}
	return rows.Err()
}

func mergeOperatorClassFunctions(ctx context.Context, q *sql.DB, schemas []string, byOID map[uint32]*operatorClassDef) error {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT opc.oid,
		       amproc.amprocnum,
		       fn_ns.nspname, fn.proname,
		       pg_catalog.pg_get_function_identity_arguments(fn.oid)
		FROM pg_opclass opc
		INNER JOIN pg_namespace n ON n.oid = opc.opcnamespace
		INNER JOIN pg_amproc amproc ON amproc.amprocfamily = opc.opcfamily
		  AND amproc.amproclefttype = opc.opcintype
		  AND amproc.amprocrighttype = opc.opcintype
		INNER JOIN pg_proc fn ON fn.oid = amproc.amproc
		INNER JOIN pg_namespace fn_ns ON fn_ns.oid = fn.pronamespace
		WHERE n.nspname IN (%s)
		  AND n.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_opclass'::regclass
		      AND d.objid = opc.oid
		      AND d.deptype = 'e'
		  )
		  AND EXISTS (
		    SELECT 1 FROM pg_depend d
		    WHERE d.classid = 'pg_amproc'::regclass
		      AND d.objid = amproc.oid
		      AND d.refclassid = 'pg_opclass'::regclass
		      AND d.refobjid = opc.oid
		      AND d.deptype = 'i'
		  )
		ORDER BY opc.oid, amproc.amprocnum`, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("list operator class functions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var oid uint32
		var fn operatorClassFunction
		if err := rows.Scan(
			&oid,
			&fn.supportNum,
			&fn.fnSchema, &fn.fnName, &fn.fnArgs,
		); err != nil {
			return fmt.Errorf("scan operator class function: %w", err)
		}
		c := byOID[oid]
		if c == nil {
			continue
		}
		c.functions = append(c.functions, fn)
	}
	return rows.Err()
}

func applyOperatorFamilies(ctx context.Context, tgtDB execer, families []operatorFamilyDef) error {
	for _, f := range families {
		stmt := formatCreateOperatorFamily(f)
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create operator family %s.%s: %w", f.schema, f.name, err)
		}
	}
	return nil
}

func applyOperatorClasses(ctx context.Context, tgtDB execer, classes []operatorClassDef) error {
	for _, c := range classes {
		stmt, ok := formatCreateOperatorClass(c)
		if !ok {
			return fmt.Errorf("operator class %s.%s has no operators or support functions", c.schema, c.name)
		}
		if _, err := tgtDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create operator class %s.%s: %w", c.schema, c.name, err)
		}
	}
	return nil
}

func operatorClassDDLItems(c operatorClassDef) []string {
	var parts []string
	for _, op := range c.operators {
		parts = append(parts, formatOperatorClassOperator(op))
	}
	for _, fn := range c.functions {
		parts = append(parts, formatOperatorClassFunction(fn))
	}
	if c.storageType != "" {
		parts = append(parts, "STORAGE "+c.storageType)
	}
	return parts
}

func formatAlterOperatorFamilyAdd(schema, name, method, item string) string {
	return fmt.Sprintf(
		"ALTER OPERATOR FAMILY %s USING %s ADD %s",
		quoteQualifiedTable(schema, name),
		quoteIdentifier(method),
		item,
	)
}

// loadLooseOperatorFamilyMembers returns ALTER OPERATOR FAMILY statements for
// family members that are not owned by an in-scope operator class. Class-owned
// members are emitted only on that class.
func loadLooseOperatorFamilyMembers(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	ops, err := loadLooseOperatorFamilyOperators(ctx, q, schemas)
	if err != nil {
		return nil, err
	}
	fns, err := loadLooseOperatorFamilyFunctions(ctx, q, schemas)
	if err != nil {
		return nil, err
	}
	return append(ops, fns...), nil
}

func loadLooseOperatorFamilyOperators(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	leftTypeSQL := fmt.Sprintf(sqlCatalogOrQualifiedType, "lt_ns", "amop.amoplefttype", "lt_ns", "lt_t")
	rightTypeSQL := fmt.Sprintf(sqlCatalogOrQualifiedType, "rt_ns", "amop.amoprighttype", "rt_ns", "rt_t")
	query := fmt.Sprintf(`
		SELECT fam_ns.nspname, fam.opfname, am.amname,
		       amop.amopstrategy,
		       opn.nspname, o.oprname,
		       %s,
		       %s,
		       amop.amoppurpose::text,
		       COALESCE(sort_ns.nspname, ''),
		       COALESCE(sort_f.opfname, '')
		FROM pg_amop amop
		INNER JOIN pg_opfamily fam ON fam.oid = amop.amopfamily
		INNER JOIN pg_namespace fam_ns ON fam_ns.oid = fam.opfnamespace
		INNER JOIN pg_am am ON am.oid = fam.opfmethod
		INNER JOIN pg_operator o ON o.oid = amop.amopopr
		INNER JOIN pg_namespace opn ON opn.oid = o.oprnamespace
		INNER JOIN pg_type lt_t ON lt_t.oid = amop.amoplefttype
		INNER JOIN pg_namespace lt_ns ON lt_ns.oid = lt_t.typnamespace
		INNER JOIN pg_type rt_t ON rt_t.oid = amop.amoprighttype
		INNER JOIN pg_namespace rt_ns ON rt_ns.oid = rt_t.typnamespace
		LEFT JOIN pg_opfamily sort_f ON sort_f.oid = amop.amopsortfamily
		LEFT JOIN pg_namespace sort_ns ON sort_ns.oid = sort_f.opfnamespace
		WHERE fam_ns.nspname IN (%s)
		  AND fam_ns.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend fd
		    WHERE fd.classid = 'pg_opfamily'::regclass
		      AND fd.objid = fam.oid
		      AND fd.deptype = 'e'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    INNER JOIN pg_opclass own ON own.oid = d.refobjid
		    INNER JOIN pg_namespace own_ns ON own_ns.oid = own.opcnamespace
		    WHERE d.classid = 'pg_amop'::regclass
		      AND d.objid = amop.oid
		      AND d.refclassid = 'pg_opclass'::regclass
		      AND d.deptype = 'i'
		      AND own_ns.nspname IN (%s)
		  )
		ORDER BY fam_ns.nspname, fam.opfname, amop.amopstrategy`, leftTypeSQL, rightTypeSQL, inClause, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list loose operator family operators: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var schema, name, method string
		var op operatorClassOperator
		if err := rows.Scan(
			&schema, &name, &method,
			&op.strategy,
			&op.opSchema, &op.opName,
			&op.leftType, &op.rightType,
			&op.purpose,
			&op.sortFamSchema, &op.sortFamName,
		); err != nil {
			return nil, fmt.Errorf("scan loose operator family operator: %w", err)
		}
		out = append(out, formatAlterOperatorFamilyAdd(schema, name, method, formatOperatorClassOperator(op)))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadLooseOperatorFamilyFunctions(ctx context.Context, q *sql.DB, schemas []string) ([]string, error) {
	inClause, args := schemaINClause(schemas)
	query := fmt.Sprintf(`
		SELECT fam_ns.nspname, fam.opfname, am.amname,
		       amproc.amprocnum,
		       fn_ns.nspname, fn.proname,
		       pg_catalog.pg_get_function_identity_arguments(fn.oid)
		FROM pg_amproc amproc
		INNER JOIN pg_opfamily fam ON fam.oid = amproc.amprocfamily
		INNER JOIN pg_namespace fam_ns ON fam_ns.oid = fam.opfnamespace
		INNER JOIN pg_am am ON am.oid = fam.opfmethod
		INNER JOIN pg_proc fn ON fn.oid = amproc.amproc
		INNER JOIN pg_namespace fn_ns ON fn_ns.oid = fn.pronamespace
		WHERE fam_ns.nspname IN (%s)
		  AND fam_ns.nspname <> 'pg_catalog'
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend fd
		    WHERE fd.classid = 'pg_opfamily'::regclass
		      AND fd.objid = fam.oid
		      AND fd.deptype = 'e'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM pg_depend d
		    INNER JOIN pg_opclass own ON own.oid = d.refobjid
		    INNER JOIN pg_namespace own_ns ON own_ns.oid = own.opcnamespace
		    WHERE d.classid = 'pg_amproc'::regclass
		      AND d.objid = amproc.oid
		      AND d.refclassid = 'pg_opclass'::regclass
		      AND d.deptype = 'i'
		      AND own_ns.nspname IN (%s)
		  )
		ORDER BY fam_ns.nspname, fam.opfname, amproc.amprocnum`, inClause, inClause)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list loose operator family functions: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var schema, name, method string
		var fn operatorClassFunction
		if err := rows.Scan(
			&schema, &name, &method,
			&fn.supportNum,
			&fn.fnSchema, &fn.fnName, &fn.fnArgs,
		); err != nil {
			return nil, fmt.Errorf("scan loose operator family function: %w", err)
		}
		out = append(out, formatAlterOperatorFamilyAdd(schema, name, method, formatOperatorClassFunction(fn)))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
