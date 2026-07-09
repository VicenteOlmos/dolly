//go:build integration

package tui

import (
	"os"
	"testing"

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
