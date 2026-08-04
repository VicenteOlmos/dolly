package clone

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

// PreflightKind categorizes a preflight failure.
type PreflightKind string

const (
	PreflightReachability PreflightKind = "reachability"
	PreflightPermission   PreflightKind = "permission"
	PreflightVersion      PreflightKind = "version"
)

// PreflightError is returned when clone preflight fails before side effects.
type PreflightError struct {
	Kind      PreflightKind
	Strategy  string
	Role      string
	Database  string
	SourceVer string
	TargetVer string
	ClientVer string
	Hint      string
	Cause     error
}

func (e *PreflightError) Unwrap() error {
	return e.Cause
}

func (e *PreflightError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "preflight %s", e.Kind)
	if e.Strategy != "" {
		fmt.Fprintf(&b, " (%s strategy)", e.Strategy)
	}
	switch e.Kind {
	case PreflightReachability:
		if e.Database != "" {
			fmt.Fprintf(&b, ": cannot reach database %q", e.Database)
		} else {
			b.WriteString(": database unreachable")
		}
	case PreflightPermission:
		if e.Role != "" && e.Database != "" {
			fmt.Fprintf(&b, ": role %q lacks required privilege on %q", e.Role, e.Database)
		} else if e.Role != "" {
			fmt.Fprintf(&b, ": role %q lacks required privilege", e.Role)
		} else {
			b.WriteString(": insufficient privileges")
		}
	case PreflightVersion:
		if e.SourceVer != "" && e.TargetVer != "" {
			fmt.Fprintf(&b, ": source major %s, target major %s", e.SourceVer, e.TargetVer)
		} else if e.SourceVer != "" && e.ClientVer != "" {
			fmt.Fprintf(&b, ": server major %s, pg_dump major %s", e.SourceVer, e.ClientVer)
		} else {
			b.WriteString(": version mismatch")
		}
	}
	if e.Cause != nil {
		fmt.Fprintf(&b, ": %s", e.Cause.Error())
	}
	if e.Hint != "" {
		fmt.Fprintf(&b, " (%s)", e.Hint)
	}
	return b.String()
}

// pgDumpVersion runs pg_dump --version. Overridable in tests.
var pgDumpVersion = defaultPgDumpVersion

func defaultPgDumpVersion() (string, error) {
	path, err := lookPath("pg_dump")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

type preflightDSNs struct {
	sourceDSN string
	targetDSN string
	adminDSN  string
	sourceDB  string
	sameInst  bool
}

func resolvePreflightDSNs(opts Options) (preflightDSNs, error) {
	sourceDB, err := ParseDBName(opts.SourceDSN)
	if err != nil {
		return preflightDSNs{}, err
	}

	targetDSN := opts.TargetDSN
	if targetDSN == "" {
		targetDSN, err = RewriteDSN(opts.SourceDSN, opts.CloneName)
	} else {
		targetDSN, err = RewriteDSN(targetDSN, opts.CloneName)
	}
	if err != nil {
		return preflightDSNs{}, err
	}

	adminDSN, err := RewriteDSN(targetDSN, "postgres")
	if err != nil {
		return preflightDSNs{}, err
	}

	same, err := SameInstance(opts.SourceDSN, targetDSN)
	if err != nil {
		return preflightDSNs{}, err
	}

	return preflightDSNs{
		sourceDSN: opts.SourceDSN,
		targetDSN: targetDSN,
		adminDSN:  adminDSN,
		sourceDB:  sourceDB,
		sameInst:  same,
	}, nil
}

// Preflight validates reachability, permissions, and versions before clone side effects.
func Preflight(ctx context.Context, opts Options, strat Strategy) error {
	dsns, err := resolvePreflightDSNs(opts)
	if err != nil {
		return err
	}
	name := strat.Name()

	sourceConn, err := sqlOpenDB(dsns.sourceDSN)
	if err != nil {
		return fmt.Errorf("open source connection: %w", err)
	}
	defer sourceConn.Close()

	if name == "physical-backup" {
		if err := pingDB(ctx, sourceConn, dsns.sourceDB, name); err != nil {
			return err
		}
		return runReplicationPreflight(ctx, sourceConn, opts)
	}

	if err := pingDB(ctx, sourceConn, dsns.sourceDB, name); err != nil {
		return err
	}

	var adminConn *sql.DB
	if !dsns.sameInst {
		adminConn, err = sqlOpenDB(dsns.adminDSN)
		if err != nil {
			return fmt.Errorf("open admin connection: %w", err)
		}
		defer adminConn.Close()
		if err := pingDB(ctx, adminConn, "postgres", name); err != nil {
			return err
		}
	}

	if name == "template" && !dsns.sameInst {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: name,
			Hint:     "use schema-replay or logical-stream for cross-server clones",
		}
	}

	cacheKey, err := permissionCacheKey(dsns, opts, name)
	if err != nil {
		return err
	}
	now := permissionCacheNow()
	if _, hit, err := lookupPermissionCache(opts.PermissionCache, cacheKey, now); err != nil {
		return fmt.Errorf("permission cache: %w", err)
	} else if !hit {
		role, err := runPermissionChecks(ctx, sourceConn, adminConn, dsns, opts, name)
		if err != nil {
			return err
		}
		entry, err := buildPermissionCacheEntry(cacheKey, dsns, opts, name, role, opts.PermissionCache.TTL)
		if err != nil {
			return err
		}
		if err := storePermissionCache(opts.PermissionCache, entry); err != nil {
			return fmt.Errorf("permission cache: %w", err)
		}
	}

	sourceMajor, err := scanServerMajor(ctx, sourceConn)
	if err != nil {
		return err
	}

	if !dsns.sameInst {
		if adminConn == nil {
			return fmt.Errorf("internal preflight: missing admin connection for version check")
		}
		targetMajor, err := scanServerMajor(ctx, adminConn)
		if err != nil {
			return err
		}
		if targetMajor < sourceMajor {
			return &PreflightError{
				Kind:      PreflightVersion,
				Strategy:  name,
				SourceVer: strconv.Itoa(sourceMajor),
				TargetVer: strconv.Itoa(targetMajor),
				Hint:      "upgrade the target PostgreSQL server to a major version >= source",
			}
		}
	}

	if name == "logical-stream" {
		return nil
	}
	if name != "schema-replay" {
		return nil
	}

	out, err := pgDumpVersion()
	if err != nil {
		return &PreflightError{
			Kind:     PreflightVersion,
			Strategy: name,
			Hint:     "install pg_dump matching the source server major version",
		}
	}
	clientMajor, err := parsePgDumpMajor(out)
	if err != nil {
		return fmt.Errorf("parse pg_dump version: %w", err)
	}
	if clientMajor != sourceMajor {
		return &PreflightError{
			Kind:      PreflightVersion,
			Strategy:  name,
			SourceVer: strconv.Itoa(sourceMajor),
			ClientVer: strconv.Itoa(clientMajor),
			Hint:      "install pg_dump client tools with the same major version as the source server",
		}
	}
	return nil
}

func runPermissionChecks(
	ctx context.Context,
	sourceConn *sql.DB,
	adminConn *sql.DB,
	dsns preflightDSNs,
	opts Options,
	strategy string,
) (string, error) {
	role, err := scanCurrentUser(ctx, sourceConn)
	if err != nil {
		return "", err
	}
	if err := scanDatabaseConnect(ctx, sourceConn, dsns.sourceDB, role, strategy); err != nil {
		return "", err
	}
	needsSchemaReplayChecks := strategy == "schema-replay"
	if needsSchemaReplayChecks {
		effectiveScope := canonicalizeEffectiveScope(SchemasFromOptions(opts))
		if err := scanSourceClonePrivileges(ctx, sourceConn, role, strategy, effectiveScope); err != nil {
			return "", err
		}
		availConn := sourceConn
		if adminConn != nil && !dsns.sameInst {
			availConn = adminConn
		}
		if err := scanRequiredExtensions(ctx, sourceConn, availConn, role, strategy); err != nil {
			return "", err
		}
	}

	if opts.SkipCreate {
		admin := adminConn
		if admin == nil {
			admin, err = sqlOpenDB(dsns.adminDSN)
			if err != nil {
				return "", fmt.Errorf("open admin connection: %w", err)
			}
			defer deferCloseDBUnlessShared(admin, sourceConn)()
		}
		if err := scanTargetExists(ctx, admin, opts.CloneName, role, strategy); err != nil {
			return "", err
		}
		targetConn, err := sqlOpenDB(dsns.targetDSN)
		if err != nil {
			return "", fmt.Errorf("open target connection: %w", err)
		}
		defer deferCloseDBUnlessShared(targetConn, sourceConn, adminConn)()
		if err := scanDatabaseConnect(ctx, targetConn, opts.CloneName, role, strategy); err != nil {
			return "", err
		}
		if needsSchemaReplayChecks {
			if err := scanTargetExtensionRestore(ctx, sourceConn, targetConn, role, strategy); err != nil {
				return "", err
			}
			if err := scanTargetSchemaRestore(ctx, sourceConn, targetConn, role, strategy); err != nil {
				return "", err
			}
		}
	} else {
		admin := adminConn
		if admin == nil {
			admin, err = sqlOpenDB(dsns.adminDSN)
			if err != nil {
				return "", fmt.Errorf("open admin connection: %w", err)
			}
			defer deferCloseDBUnlessShared(admin, sourceConn)()
		}
		if err := scanCreateDB(ctx, admin, role, strategy); err != nil {
			return "", err
		}
		if needsSchemaReplayChecks {
			if err := scanTargetExtensionAdminHeuristic(ctx, sourceConn, admin, role, strategy); err != nil {
				return "", err
			}
		}
	}

	if needsSchemaReplayChecks {
		if err := requireClientTools(strategy); err != nil {
			return "", err
		}
	}
	return role, nil
}

// deferCloseDBUnlessShared returns a defer func that closes db unless it is the same
// pointer as one of the shared connections Preflight still needs (e.g. sqlmock in tests).
func deferCloseDBUnlessShared(db *sql.DB, shared ...*sql.DB) func() {
	return func() {
		for _, s := range shared {
			if s != nil && db == s {
				return
			}
		}
		db.Close()
	}
}

func runReplicationPreflight(ctx context.Context, sourceConn *sql.DB, opts Options) error {
	const strategy = "physical-backup"

	if opts.TargetDir == "" {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "set --target-dir or clone.target_dir to an empty or non-existent path",
		}
	}
	if err := validateReplicationTargetDir(opts.TargetDir); err != nil {
		return err
	}

	var walLevel string
	if err := sourceConn.QueryRowContext(ctx, `SHOW wal_level`).Scan(&walLevel); err != nil {
		return fmt.Errorf("check wal_level: %w", err)
	}
	if !walLevelAtLeastReplica(walLevel) {
		return &PreflightError{
			Kind:     PreflightVersion,
			Strategy: strategy,
			Hint:     fmt.Sprintf("wal_level is %q; set wal_level = replica (or logical) and restart PostgreSQL", walLevel),
		}
	}

	var maxWalSenders int
	if err := sourceConn.QueryRowContext(ctx, `SHOW max_wal_senders`).Scan(&maxWalSenders); err != nil {
		return fmt.Errorf("check max_wal_senders: %w", err)
	}
	if maxWalSenders < 2 {
		return &PreflightError{
			Kind:     PreflightVersion,
			Strategy: strategy,
			Hint:     fmt.Sprintf("max_wal_senders is %d; set max_wal_senders >= 2 and reload PostgreSQL", maxWalSenders),
		}
	}

	role, err := scanCurrentUser(ctx, sourceConn)
	if err != nil {
		return err
	}
	var rolreplication, rolsuper bool
	if err := sourceConn.QueryRowContext(ctx,
		`SELECT rolreplication, rolsuper FROM pg_roles WHERE rolname = current_user`,
	).Scan(&rolreplication, &rolsuper); err != nil {
		return fmt.Errorf("check REPLICATION privilege: %w", err)
	}
	if !rolreplication && !rolsuper {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Role:     role,
			Hint:     fmt.Sprintf("grant REPLICATION to role %q: ALTER ROLE %s WITH REPLICATION", role, role),
		}
	}

	if _, err := lookPath("pg_basebackup"); err != nil {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "install PostgreSQL client tools (pg_basebackup) and ensure they are on PATH",
		}
	}

	return nil
}

func walLevelAtLeastReplica(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "replica", "logical":
		return true
	default:
		return false
	}
}

var errReplicationTargetNotEmpty = errors.New("replication target directory is not empty")

// checkReplicationTargetDir returns nil when targetDir is absent or an empty directory.
func checkReplicationTargetDir(targetDir string) error {
	info, err := os.Stat(targetDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat target directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target path is not a directory: %s", targetDir)
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return fmt.Errorf("read target directory: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", errReplicationTargetNotEmpty, targetDir)
	}
	return nil
}

// ensureReplicationTargetDir creates targetDir when absent or reuses an empty directory.
func ensureReplicationTargetDir(targetDir string) error {
	if err := checkReplicationTargetDir(targetDir); err != nil {
		if errors.Is(err, errReplicationTargetNotEmpty) {
			return fmt.Errorf("target directory is not empty: %s", targetDir)
		}
		return err
	}
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		if err := os.Mkdir(targetDir, 0o700); err != nil {
			return fmt.Errorf("create target directory: %w", err)
		}
	}
	return nil
}

func validateReplicationTargetDir(targetDir string) error {
	const strategy = "physical-backup"

	err := checkReplicationTargetDir(targetDir)
	if err == nil {
		return nil
	}
	if errors.Is(err, errReplicationTargetNotEmpty) {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "provide an empty or non-existent directory for pg_basebackup -D",
		}
	}
	if strings.Contains(err.Error(), "not a directory") {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "target path must be a directory for pg_basebackup -D",
		}
	}
	return err
}

func pingDB(ctx context.Context, dbConn *sql.DB, dbLabel, strategy string) error {
	if err := dbConn.PingContext(ctx); err != nil {
		return &PreflightError{
			Kind:     PreflightReachability,
			Strategy: strategy,
			Database: dbLabel,
			Cause:    errors.New(connections.RedactMessage(err.Error())),
			Hint:     "check host, port, credentials, and network access",
		}
	}
	return nil
}

func scanCurrentUser(ctx context.Context, dbConn *sql.DB) (string, error) {
	var role string
	if err := dbConn.QueryRowContext(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return "", fmt.Errorf("query current_user: %w", err)
	}
	return role, nil
}

func scanDatabaseConnect(ctx context.Context, dbConn *sql.DB, dbName, role, strategy string) error {
	var ok bool
	err := dbConn.QueryRowContext(ctx,
		`SELECT has_database_privilege(current_user, $1, 'CONNECT')`, dbName,
	).Scan(&ok)
	if err != nil {
		return fmt.Errorf("check CONNECT on %q: %w", dbName, err)
	}
	if !ok {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Role:     role,
			Database: dbName,
			Hint:     "grant CONNECT on the database to this role",
		}
	}
	return nil
}

func scanCreateDB(ctx context.Context, dbConn *sql.DB, role, strategy string) error {
	var canCreate bool
	err := dbConn.QueryRowContext(ctx,
		`SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user`,
	).Scan(&canCreate)
	if err != nil {
		return fmt.Errorf("check CREATEDB: %w", err)
	}
	if !canCreate {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Role:     role,
			Database: "postgres (CREATEDB required)",
			Hint:     "grant CREATEDB to this role or use skip_create with an existing target database",
		}
	}
	return nil
}

func scanTargetExists(ctx context.Context, dbConn *sql.DB, cloneName, role, strategy string) error {
	var exists int
	err := dbConn.QueryRowContext(ctx,
		`SELECT 1 FROM pg_database WHERE datname = $1`, cloneName,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Role:     role,
			Database: cloneName,
			Hint:     "create the target database first or omit skip_create",
		}
	}
	if err != nil {
		return fmt.Errorf("check target database exists: %w", err)
	}
	return nil
}

const userSchemaFilter = `
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND n.nspname NOT LIKE 'pg_temp_%'
		  AND n.nspname NOT LIKE 'pg_toast_%'`

func scopedNamespacePredicate(column string, scope []string) (string, []any) {
	if len(scope) == 0 {
		return "", nil
	}
	return fmt.Sprintf(" AND %s = ANY($1::text[])", column), []any{scope}
}

func queryRowContextOptional(ctx context.Context, dbConn *sql.DB, query string, args []any) *sql.Row {
	if len(args) == 0 {
		return dbConn.QueryRowContext(ctx, query)
	}
	return dbConn.QueryRowContext(ctx, query, args...)
}

const userRelationFilter = `
		  AND c.relname NOT LIKE 'pg_toast_%'`

func scanSourceClonePrivileges(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	if err := scanRelationReadPrivileges(ctx, dbConn, role, strategy, scope); err != nil {
		return err
	}
	if err := scanForeignKeyReferencedRead(ctx, dbConn, role, strategy, scope); err != nil {
		return err
	}
	if err := scanSequencePrivileges(ctx, dbConn, role, strategy, scope); err != nil {
		return err
	}
	if err := scanSchemaUsage(ctx, dbConn, role, strategy, scope); err != nil {
		return err
	}
	if err := scanTypeUsage(ctx, dbConn, role, strategy, scope); err != nil {
		return err
	}
	return scanFunctionVisibility(ctx, dbConn, role, strategy, scope)
}

func scanSchemaUsage(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	var schema string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT n.nspname
		FROM pg_namespace n
		WHERE TRUE
		`+userSchemaFilter+scopePred+`
		  AND EXISTS (
		    SELECT 1 FROM pg_class c
		    WHERE c.relnamespace = n.oid
		      AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
		  )
		  AND NOT has_schema_privilege(n.oid, 'USAGE')
		ORDER BY n.nspname
		LIMIT 1`, scopeArgs).Scan(&schema)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check schema USAGE privileges: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: schema + " (schema)",
		Hint:     "grant USAGE on schema " + schema,
	}
}

func scanTypeUsage(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	var schema, typ string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT n.nspname, t.typname
		FROM pg_type t
		INNER JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE TRUE
		`+userSchemaFilter+scopePred+`
		  AND t.typtype IN ('c', 'd', 'e')
		  AND NOT has_type_privilege(t.oid, 'USAGE')
		ORDER BY n.nspname, t.typname
		LIMIT 1`, scopeArgs).Scan(&schema, &typ)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check type USAGE privileges: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: schema + "." + typ + " (type)",
		Hint:     "grant USAGE on type " + schema + "." + typ,
	}
}

func scanFunctionVisibility(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	var schema, fn string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT n.nspname, p.proname
		FROM pg_proc p
		INNER JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE TRUE
		`+userSchemaFilter+scopePred+`
		  AND p.prokind IN ('f', 'p')
		  AND NOT (
		    pg_has_role(current_user, p.proowner, 'USAGE')
		    OR (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)
		  )
		ORDER BY n.nspname, p.proname
		LIMIT 1`, scopeArgs).Scan(&schema, &fn)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check function dump visibility: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: schema + "." + fn + " (function)",
		Hint:     "grant sufficient privileges to dump function " + schema + "." + fn + " (owner, superuser, or role membership)",
	}
}

func scanTargetExtensionRestore(ctx context.Context, sourceConn, targetConn *sql.DB, role, strategy string) error {
	rows, err := sourceConn.QueryContext(ctx, `
		SELECT extname
		FROM pg_extension
		WHERE extname <> 'plpgsql'
		ORDER BY extname`)
	if err != nil {
		return fmt.Errorf("list source extensions for target restore: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			return fmt.Errorf("scan extension name: %w", err)
		}
		var installed bool
		if err := targetConn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, ext,
		).Scan(&installed); err != nil {
			return fmt.Errorf("check extension %q installed on target: %w", ext, err)
		}
		if installed {
			continue
		}
		var creatable bool
		if err := targetConn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = $1)
			  AND (
			    (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)
			    OR has_database_privilege(current_database(), 'CREATE')
			  )`, ext,
		).Scan(&creatable); err != nil {
			return fmt.Errorf("check extension %q creatable on target: %w", ext, err)
		}
		if !creatable {
			return &PreflightError{
				Kind:     PreflightPermission,
				Strategy: strategy,
				Role:     role,
				Database: ext + " (extension)",
				Hint:     "install extension " + ext + " on the target database or grant CREATE on the database so the role can CREATE EXTENSION",
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list source extensions for target restore: %w", err)
	}
	return nil
}

func scanTargetSchemaRestore(ctx context.Context, sourceConn, targetConn *sql.DB, role, strategy string) error {
	rows, err := sourceConn.QueryContext(ctx, `
		SELECT DISTINCT n.nspname
		FROM pg_namespace n
		INNER JOIN pg_class c ON c.relnamespace = n.oid
		WHERE c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
		`+userSchemaFilter+`
		ORDER BY n.nspname`)
	if err != nil {
		return fmt.Errorf("list source restore schemas: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schema string
		if err := rows.Scan(&schema); err != nil {
			return fmt.Errorf("scan schema name: %w", err)
		}
		var exists bool
		if err := targetConn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema,
		).Scan(&exists); err != nil {
			return fmt.Errorf("check schema %q exists on target: %w", schema, err)
		}
		if !exists {
			var canCreateDB bool
			if err := targetConn.QueryRowContext(ctx, `
				SELECT has_database_privilege(current_database(), 'CREATE')`,
			).Scan(&canCreateDB); err != nil {
				return fmt.Errorf("check CREATE on target database: %w", err)
			}
			if !canCreateDB {
				return &PreflightError{
					Kind:     PreflightPermission,
					Strategy: strategy,
					Role:     role,
					Database: schema + " (schema)",
					Hint:     "grant CREATE on the target database so schema " + schema + " can be created during restore",
				}
			}
			continue
		}
		var hasUsage bool
		if err := targetConn.QueryRowContext(ctx, `
			SELECT has_schema_privilege($1::regnamespace, 'USAGE')`, schema,
		).Scan(&hasUsage); err != nil {
			return fmt.Errorf("check USAGE on target schema %q: %w", schema, err)
		}
		if !hasUsage {
			return &PreflightError{
				Kind:     PreflightPermission,
				Strategy: strategy,
				Role:     role,
				Database: schema + " (schema)",
				Hint:     "grant USAGE on schema " + schema + " in the target database",
			}
		}
		var hasCreate bool
		if err := targetConn.QueryRowContext(ctx, `
			SELECT has_schema_privilege($1::regnamespace, 'CREATE')`, schema,
		).Scan(&hasCreate); err != nil {
			return fmt.Errorf("check CREATE on target schema %q: %w", schema, err)
		}
		if !hasCreate {
			return &PreflightError{
				Kind:     PreflightPermission,
				Strategy: strategy,
				Role:     role,
				Database: schema + " (schema)",
				Hint:     "grant CREATE on schema " + schema + " in the target database",
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list source restore schemas: %w", err)
	}
	return nil
}

func scanTargetExtensionAdminHeuristic(ctx context.Context, sourceConn, adminConn *sql.DB, role, strategy string) error {
	rows, err := sourceConn.QueryContext(ctx, `
		SELECT extname
		FROM pg_extension
		WHERE extname <> 'plpgsql'
		ORDER BY extname`)
	if err != nil {
		return fmt.Errorf("list source extensions for admin heuristic: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			return fmt.Errorf("scan extension name: %w", err)
		}
		var ok bool
		if err := adminConn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = $1)
			  AND (
			    (SELECT rolsuper FROM pg_roles WHERE rolname = current_user)
			    OR (SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user)
			  )`, ext,
		).Scan(&ok); err != nil {
			return fmt.Errorf("check admin CREATE EXTENSION heuristic for %q: %w", ext, err)
		}
		if !ok {
			return &PreflightError{
				Kind:     PreflightPermission,
				Strategy: strategy,
				Role:     role,
				Database: ext + " (extension)",
				Hint:     "grant superuser or CREATEDB to this role, pre-install extension " + ext + " on the target server, or use skip_create with a prepared target database",
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list source extensions for admin heuristic: %w", err)
	}
	return nil
}

func scanRelationReadPrivileges(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	var schema, name, kind string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT n.nspname, c.relname, c.relkind
		FROM pg_class c
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p', 'v', 'm')
		`+userSchemaFilter+userRelationFilter+scopePred+`
		  AND NOT has_table_privilege(c.oid, 'SELECT')
		ORDER BY n.nspname, c.relname
		LIMIT 1`, scopeArgs).Scan(&schema, &name, &kind)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check relation read privileges: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: schema + "." + name + " (" + relationKindLabel(kind) + ")",
		Hint:     "grant SELECT on tables, views, and materialized views needed for clone",
	}
}

func scanForeignKeyReferencedRead(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("n.nspname", scope)
	var fromSchema, fromTable, refSchema, refTable string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT n.nspname, c.relname, ref_n.nspname, ref_c.relname
		FROM pg_constraint con
		INNER JOIN pg_class c ON c.oid = con.conrelid
		INNER JOIN pg_namespace n ON n.oid = c.relnamespace
		INNER JOIN pg_class ref_c ON ref_c.oid = con.confrelid
		INNER JOIN pg_namespace ref_n ON ref_n.oid = ref_c.relnamespace
		WHERE con.contype = 'f'
		`+userSchemaFilter+scopePred+`
		  AND c.relname NOT LIKE 'pg_toast_%'
		  AND ref_c.relname NOT LIKE 'pg_toast_%'
		  AND ref_n.nspname NOT IN ('pg_catalog', 'information_schema')
		  AND ref_n.nspname NOT LIKE 'pg_temp_%'
		  AND ref_n.nspname NOT LIKE 'pg_toast_%'
		  AND NOT has_table_privilege(ref_c.oid, 'SELECT')
		ORDER BY n.nspname, c.relname
		LIMIT 1`, scopeArgs).Scan(&fromSchema, &fromTable, &refSchema, &refTable)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check foreign key referenced read privileges: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: refSchema + "." + refTable,
		Hint:     fmt.Sprintf("grant SELECT on referenced table for FK %s.%s -> %s.%s", fromSchema, fromTable, refSchema, refTable),
	}
}

func scanRequiredExtensions(ctx context.Context, sourceConn, availConn *sql.DB, role, strategy string) error {
	rows, err := sourceConn.QueryContext(ctx, `
		SELECT extname
		FROM pg_extension
		WHERE extname <> 'plpgsql'
		ORDER BY extname`)
	if err != nil {
		return fmt.Errorf("list source extensions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			return fmt.Errorf("scan extension name: %w", err)
		}
		var available bool
		if err := availConn.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = $1)`, ext,
		).Scan(&available); err != nil {
			return fmt.Errorf("check extension %q availability: %w", ext, err)
		}
		if !available {
			return &PreflightError{
				Kind:     PreflightPermission,
				Strategy: strategy,
				Role:     role,
				Database: ext + " (extension)",
				Hint:     "install the extension on the target PostgreSQL server before clone",
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list source extensions: %w", err)
	}
	return nil
}

func scanSequencePrivileges(ctx context.Context, dbConn *sql.DB, role, strategy string, scope []string) error {
	scopePred, scopeArgs := scopedNamespacePredicate("ps.schemaname", scope)
	var schema, name string
	err := queryRowContextOptional(ctx, dbConn, `
		SELECT ps.schemaname, ps.sequencename
		FROM pg_sequences ps
		WHERE ps.schemaname NOT IN ('pg_catalog', 'information_schema')
		  AND ps.schemaname NOT LIKE 'pg_temp_%'
		  AND ps.schemaname NOT LIKE 'pg_toast_%'
		  AND ps.sequencename NOT LIKE 'pg_toast_%'`+scopePred+`
		  AND NOT has_sequence_privilege(
		    (quote_ident(ps.schemaname) || '.' || quote_ident(ps.sequencename))::regclass,
		    'USAGE'
		  )
		ORDER BY ps.schemaname, ps.sequencename
		LIMIT 1`, scopeArgs).Scan(&schema, &name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check sequence privileges: %w", err)
	}
	return &PreflightError{
		Kind:     PreflightPermission,
		Strategy: strategy,
		Role:     role,
		Database: schema + "." + name + " (sequence)",
		Hint:     "grant USAGE (and SELECT if required) on sequences used by cloned tables",
	}
}

func relationKindLabel(kind string) string {
	switch kind {
	case "v":
		return "view"
	case "m":
		return "materialized view"
	case "p":
		return "partitioned table"
	default:
		return "table"
	}
}

func requireClientTools(strategy string) error {
	if _, err := lookPath("pg_dump"); err != nil {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "install PostgreSQL client tools (pg_dump) and ensure they are on PATH",
		}
	}
	if _, err := lookPath("psql"); err != nil {
		return &PreflightError{
			Kind:     PreflightPermission,
			Strategy: strategy,
			Hint:     "install PostgreSQL client tools (psql) and ensure they are on PATH",
		}
	}
	return nil
}

func scanServerMajor(ctx context.Context, dbConn *sql.DB) (int, error) {
	var versionNum int
	if err := dbConn.QueryRowContext(ctx, `SHOW server_version_num`).Scan(&versionNum); err != nil {
		return 0, fmt.Errorf("read server_version_num: %w", err)
	}
	return parseServerMajor(versionNum)
}

func parseServerMajor(versionNum int) (int, error) {
	if versionNum <= 0 {
		return 0, fmt.Errorf("invalid server_version_num %d", versionNum)
	}
	return versionNum / 10000, nil
}

var pgDumpMajorRE = regexp.MustCompile(`(?:PostgreSQL|postgres)\s+(\d+)`)

func parsePgDumpMajor(output string) (int, error) {
	m := pgDumpMajorRE.FindStringSubmatch(output)
	if len(m) < 2 {
		if i := strings.Index(output, ") "); i >= 0 {
			fields := strings.Fields(output[i+2:])
			if len(fields) > 0 {
				major, err := strconv.Atoi(strings.Split(fields[0], ".")[0])
				if err == nil {
					return major, nil
				}
			}
		}
		return 0, fmt.Errorf("cannot parse pg_dump version from %q", strings.TrimSpace(output))
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, err
	}
	return major, nil
}
