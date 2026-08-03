package tui

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/db"
)

type connectRequestedMsg struct {
	dsn     string
	schemas []string
}

type connectResultMsg struct {
	db                *sql.DB
	tables            []db.Table
	sourceSchemaNames []string
	err               error
}

type testConnectionRequestedMsg struct {
	dsn string
}

type testConnectionResultMsg struct {
	err error
}

// SchemaLoader connects to PostgreSQL and loads schema metadata.
type SchemaLoader interface {
	ConnectAndLoad(ctx context.Context, dsn string, schemas []string) (*sql.DB, []db.Table, error)
	ConnectForSchemaPicker(ctx context.Context, dsn string) (*sql.DB, []string, error)
	LoadTables(ctx context.Context, db *sql.DB, schemas []string) ([]db.Table, error)
	Ping(ctx context.Context, dsn string) error
}

type dbConnOptions struct {
	statementTimeout string
	maxOpenConns     int
}

func (o dbConnOptions) effectiveMaxOpenConns() int {
	if o.maxOpenConns <= 0 {
		return 5
	}
	return o.maxOpenConns
}

func (o dbConnOptions) prepareDSN(dsn string) (string, error) {
	if o.statementTimeout == "" || o.statementTimeout == "0" {
		return dsn, nil
	}
	return connections.SetDSNParam(dsn, "statement_timeout", o.statementTimeout)
}

type postgresSchemaLoader struct {
	dbConnOptions
}

func defaultPostgresSchemaLoader() postgresSchemaLoader {
	return postgresSchemaLoader{dbConnOptions: dbConnOptions{maxOpenConns: 5}}
}

// ensureConnectTimeout injects connect_timeout=10 into the DSN unless it is
// already present. Handles both URL form (postgres://...) and libpq keyword
// form (host=localhost port=5432). Unknown/garbage input is returned unchanged.
func ensureConnectTimeout(dsn string) string {
	// URL form: postgres:// or postgresql://
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if q.Get("connect_timeout") != "" {
			return dsn
		}
		q.Set("connect_timeout", "10")
		u.RawQuery = q.Encode()
		return u.String()
	}
	// Keyword form: host=localhost port=5432 dbname=mydb
	// Detected by presence of '=' and absence of '://' (rules out URLs and garbage).
	if strings.Contains(dsn, "=") && !strings.Contains(dsn, "://") {
		if strings.Contains(dsn, "connect_timeout") {
			return dsn
		}
		return dsn + " connect_timeout=10"
	}
	return dsn
}

func (l postgresSchemaLoader) openAndPing(ctx context.Context, dsn string) (*sql.DB, error) {
	prepared, err := l.prepareDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("configure connection: %s", connections.RedactMessage(err.Error()))
	}
	conn, err := sql.Open("pgx", ensureConnectTimeout(prepared))
	if err != nil {
		return nil, fmt.Errorf("open connection: %w", err)
	}
	conn.SetMaxOpenConns(l.effectiveMaxOpenConns())
	conn.SetConnMaxIdleTime(5 * time.Minute)
	conn.SetConnMaxLifetime(30 * time.Minute)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping: %s", connections.RedactMessage(err.Error()))
	}
	return conn, nil
}

func (l postgresSchemaLoader) Ping(ctx context.Context, dsn string) error {
	conn, err := l.openAndPing(ctx, dsn)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (l postgresSchemaLoader) ConnectAndLoad(ctx context.Context, dsn string, schemas []string) (*sql.DB, []db.Table, error) {
	conn, err := l.openAndPing(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	tables, err := db.LoadPostgresSchemas(ctx, conn, schemas)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("load schema: %w", err)
	}
	return conn, tables, nil
}

func (l postgresSchemaLoader) LoadTables(ctx context.Context, dbConn *sql.DB, schemas []string) ([]db.Table, error) {
	return db.LoadPostgresSchemas(ctx, dbConn, schemas)
}

func (l postgresSchemaLoader) ConnectForSchemaPicker(ctx context.Context, dsn string) (*sql.DB, []string, error) {
	conn, err := l.openAndPing(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	names, err := db.ListPostgresSchemaNames(ctx, conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("list schemas: %w", err)
	}
	return conn, names, nil
}

func ConnectCmd(loader SchemaLoader, dsn string, schemas []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, names, err := loader.ConnectForSchemaPicker(ctx, dsn)
		if err != nil {
			return connectResultMsg{err: err}
		}
		tables, err := loader.LoadTables(ctx, conn, schemas)
		if err != nil {
			_ = conn.Close()
			return connectResultMsg{err: fmt.Errorf("load schema: %w", err)}
		}
		return connectResultMsg{
			db:                conn,
			tables:            tables,
			sourceSchemaNames: names,
		}
	}
}

func TestConnectionCmd(loader SchemaLoader, dsn string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := loader.Ping(ctx, dsn)
		return testConnectionResultMsg{err: err}
	}
}

func schemaDraftFromTables(tables []db.Table) SchemaDraft {
	draft := SchemaDraft{TableCount: len(tables)}
	draft.Tables = make([]string, len(tables))
	for i, table := range tables {
		if table.Schema != "" && table.Schema != "public" {
			draft.Tables[i] = table.Schema + "." + table.Name
		} else {
			draft.Tables[i] = table.Name
		}
		draft.ColumnCount += len(table.Columns)
		draft.FKCount += len(table.ForeignKeys)
	}
	return draft
}
