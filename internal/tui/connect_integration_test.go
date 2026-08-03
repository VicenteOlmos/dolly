//go:build integration

package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

func TestConnectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pgintegration.Open(t)
	dsn := os.Getenv(pgintegration.EnvDSN)

	app := NewApp()
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, connectRequestedMsg{dsn: dsn})

	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	if app.schema.TableCount == 0 {
		t.Fatal("expected loaded tables")
	}
	if app.db == nil {
		t.Fatal("expected session db")
	}
}

func TestPingIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pgintegration.Open(t)
	dsn := os.Getenv(pgintegration.EnvDSN)

	app := NewApp()
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, testConnectionRequestedMsg{dsn: dsn})

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
	if app.db != nil {
		t.Fatal("ping-only test must not retain session db")
	}
	if app.schema.TableCount != 0 {
		t.Fatal("ping-only test must not load schema")
	}
	if app.connStatus != ConnStatusIdle {
		t.Fatalf("connStatus = %v, want idle after successful ping", app.connStatus)
	}
	if !containsPlain(app.statusMsg, "Connection OK") {
		t.Fatalf("statusMsg = %q, want Connection OK", stripANSIForGolden(app.statusMsg))
	}
}

func TestConfigAwareConnectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pgintegration.Open(t)
	dsn := os.Getenv(pgintegration.EnvDSN)

	cfg := config.DefaultConfig()
	cfg.DB.StatementTimeout = "2min"
	cfg.DB.MaxOpenConns = 3

	app := NewAppFromConfig(nil, false, cfg, "")
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, connectRequestedMsg{dsn: dsn})

	if app.db == nil {
		t.Fatal("expected session db")
	}
	defer app.closeDB()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var timeout string
	if err := app.db.QueryRowContext(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil {
		t.Fatalf("SHOW statement_timeout: %v", err)
	}
	if timeout != "2min" {
		t.Fatalf("statement_timeout = %q, want 2min", timeout)
	}
	if app.db.Stats().MaxOpenConnections != 3 {
		t.Fatalf("MaxOpenConnections = %d, want 3", app.db.Stats().MaxOpenConnections)
	}
}

func TestConfigAwareLoaderDisabledTimeoutIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pgintegration.Open(t)
	dsn := os.Getenv(pgintegration.EnvDSN)

	cfg := config.DefaultConfig()
	cfg.DB.StatementTimeout = "0"
	cfg.DB.MaxOpenConns = 4

	loader := postgresSchemaLoaderFromConfig(cfg)
	conn, err := loader.openAndPing(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var timeout string
	if err := conn.QueryRowContext(ctx, "SHOW statement_timeout").Scan(&timeout); err != nil {
		t.Fatalf("SHOW statement_timeout: %v", err)
	}
	if timeout != "0" {
		t.Fatalf("statement_timeout = %q, want 0 when disabled", timeout)
	}
	if conn.Stats().MaxOpenConnections != 4 {
		t.Fatalf("MaxOpenConnections = %d, want 4", conn.Stats().MaxOpenConnections)
	}
	if strings.Contains(dsn, "statement_timeout") {
		t.Fatalf("base DSN should not require timeout param: %q", dsn)
	}
}
