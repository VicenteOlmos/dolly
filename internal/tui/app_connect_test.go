package tui

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/db"
)

type mockSchemaLoader struct {
	tables      []db.Table
	schemaNames []string
	err         error
	pingErr     error
}

func (m mockSchemaLoader) ConnectAndLoad(_ context.Context, _ string, _ []string) (*sql.DB, []db.Table, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		return nil, nil, err
	}
	return conn, m.tables, nil
}

func (m mockSchemaLoader) LoadTables(_ context.Context, _ *sql.DB, _ []string) ([]db.Table, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tables, nil
}

func (m mockSchemaLoader) ConnectForSchemaPicker(_ context.Context, _ string) (*sql.DB, []string, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		return nil, nil, err
	}
	names := m.schemaNames
	if len(names) == 0 {
		names = []string{"public", "app"}
	}
	return conn, names, nil
}

func (m mockSchemaLoader) Ping(_ context.Context, _ string) error {
	return m.pingErr
}

func TestNewAppFromConfigUsesConfigAwareLoader(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DB.MaxOpenConns = 12
	cfg.DB.StatementTimeout = "2min"

	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	loader, ok := app.loader.(postgresSchemaLoader)
	if !ok {
		t.Fatalf("loader type = %T, want postgresSchemaLoader", app.loader)
	}
	if loader.maxOpenConns != 12 {
		t.Fatalf("maxOpenConns = %d, want 12", loader.maxOpenConns)
	}
	if loader.statementTimeout != "2min" {
		t.Fatalf("statementTimeout = %q, want 2min", loader.statementTimeout)
	}
}

func TestNewAppUsesDefaultPoolSize(t *testing.T) {
	app := NewApp()
	loader, ok := app.loader.(postgresSchemaLoader)
	if !ok {
		t.Fatalf("loader type = %T, want postgresSchemaLoader", app.loader)
	}
	if loader.effectiveMaxOpenConns() != 5 {
		t.Fatalf("effectiveMaxOpenConns = %d, want 5", loader.effectiveMaxOpenConns())
	}
	if loader.statementTimeout != "" {
		t.Fatalf("statementTimeout = %q, want empty", loader.statementTimeout)
	}
}

func drainUpdate(app *App, msg tea.Msg) *App {
	next, cmd := app.Update(msg)
	app = next.(*App)
	for cmd != nil {
		m := cmd()
		next, cmd = app.Update(m)
		app = next.(*App)
	}
	return app
}

func TestAppConnectSuccess(t *testing.T) {
	loader := mockSchemaLoader{tables: []db.Table{
		{Name: "orders", Columns: []db.Column{{Name: "id"}, {Name: "total"}}},
		{Name: "users", Columns: []db.Column{{Name: "id"}}},
	}}
	app := NewAppWithLoader(loader)
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	if app.connStatus != ConnStatusConnected {
		t.Fatalf("connStatus = %v, want connected", app.connStatus)
	}
	if app.schema.TableCount != 2 {
		t.Fatalf("TableCount = %d, want 2", app.schema.TableCount)
	}
	if app.schema.ColumnCount != 3 {
		t.Fatalf("ColumnCount = %d, want 3", app.schema.ColumnCount)
	}
	if app.db == nil {
		t.Fatal("expected session db")
	}
}

func TestAppConnectFailure(t *testing.T) {
	loader := mockSchemaLoader{err: errors.New("connection refused")}
	app := NewAppWithLoader(loader)
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
	if app.connStatus != ConnStatusError {
		t.Fatalf("connStatus = %v, want error", app.connStatus)
	}
	if app.connectError == "" {
		t.Fatal("expected connect error message")
	}
	if app.db != nil {
		t.Fatal("expected no session db on failure")
	}
	if app.statusMsg == "" {
		t.Fatal("expected status bar message on failure")
	}
	if !containsPlain(app.statusMsg, "Connection failed") {
		t.Fatalf("statusMsg = %q, want connection failed hint", stripANSIForGolden(app.statusMsg))
	}
	if !containsPlain(app.View().Content, "Connection failed") {
		t.Fatalf("view missing connection failed status: %q", stripANSIForGolden(app.View().Content))
	}
}

func TestAppConnectStatusBarWhileConnecting(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{tables: []db.Table{{Name: "users"}}})
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	next, cmd := app.Update(keyPress("", tea.KeyEnter, 0))
	app = next.(*App)
	if cmd == nil {
		t.Fatal("expected cmd from Enter on connection screen")
	}

	next, cmd = app.Update(cmd())
	app = next.(*App)
	if cmd == nil {
		t.Fatal("expected ConnectCmd after connect requested")
	}
	if app.statusMsg != "Connecting…" {
		t.Fatalf("statusMsg = %q, want Connecting…", app.statusMsg)
	}
	if app.connStatus != ConnStatusConnecting {
		t.Fatalf("connStatus = %v, want connecting", app.connStatus)
	}
	if !containsPlain(app.View().Content, "Connecting…") {
		t.Fatalf("view missing connecting status: %q", stripANSIForGolden(app.View().Content))
	}
}

func TestAppConnectIgnoresDuplicateSubmit(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{})
	app.connStatus = ConnStatusConnecting

	next, cmd := app.Update(connectRequestedMsg{dsn: "postgres://u:p@h-x/db_stub"})
	if cmd != nil {
		t.Fatal("expected no command while already connecting")
	}
	got := next.(*App)
	if got.connStatus != ConnStatusConnecting {
		t.Fatalf("connStatus = %v, want connecting", got.connStatus)
	}
}

func TestAppTestConnectionSuccess(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{})
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", 't', tea.ModCtrl))

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
	if app.connStatus != ConnStatusIdle {
		t.Fatalf("connStatus = %v, want idle", app.connStatus)
	}
	if app.db != nil {
		t.Fatal("expected no session db after ping-only test")
	}
	if app.schema.TableCount != 0 {
		t.Fatalf("TableCount = %d, want 0 (no schema load)", app.schema.TableCount)
	}
	if !containsPlain(app.statusMsg, "Connection OK") {
		t.Fatalf("statusMsg = %q, want Connection OK", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppTestConnectionFailure(t *testing.T) {
	loader := mockSchemaLoader{pingErr: errors.New("connection refused")}
	app := NewAppWithLoader(loader)
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", 't', tea.ModCtrl))

	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection", app.screen)
	}
	if app.connStatus != ConnStatusError {
		t.Fatalf("connStatus = %v, want error", app.connStatus)
	}
	if app.connectError == "" {
		t.Fatal("expected connect error message in pane")
	}
	if app.db != nil {
		t.Fatal("expected no session db on test failure")
	}
	if !containsPlain(app.statusMsg, "Connection failed") {
		t.Fatalf("statusMsg = %q, want connection failed hint", stripANSIForGolden(app.statusMsg))
	}
}

func TestAppTestConnectionIgnoresDuplicateSubmit(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{})
	app.connStatus = ConnStatusConnecting

	next, cmd := app.Update(testConnectionRequestedMsg{dsn: "postgres://u:p@h-x/db_stub"})
	if cmd != nil {
		t.Fatal("expected no command while already connecting")
	}
	got := next.(*App)
	if got.connStatus != ConnStatusConnecting {
		t.Fatalf("connStatus = %v, want connecting", got.connStatus)
	}
}

func TestAppTestConnectionBlocksConnectWhileBusy(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{})
	app.screen = ScreenConnection
	app.width = 80
	app.height = 24

	next, cmd := app.Update(keyPress("", 't', tea.ModCtrl))
	app = next.(*App)
	if cmd == nil {
		t.Fatal("expected cmd from Ctrl+t on connection screen")
	}
	next, pingCmd := app.Update(cmd())
	app = next.(*App)
	if pingCmd == nil {
		t.Fatal("expected TestConnectionCmd after test requested")
	}
	if app.connStatus != ConnStatusConnecting {
		t.Fatalf("connStatus = %v, want connecting during ping", app.connStatus)
	}

	next, enterCmd := app.Update(keyPress("", tea.KeyEnter, 0))
	if enterCmd != nil {
		t.Fatal("expected no connect cmd while test in flight")
	}
	if next.(*App).connStatus != ConnStatusConnecting {
		t.Fatal("connect should not start while ping in flight")
	}

	app = drainUpdate(app, pingCmd())
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection after ping", app.screen)
	}
	if !containsPlain(app.statusMsg, "Connection OK") {
		t.Fatalf("statusMsg = %q, want Connection OK", stripANSIForGolden(app.statusMsg))
	}
}

func TestSchemaScreenWithoutSession(t *testing.T) {
	app := NewApp()
	app.width = 80
	app.height = 24

	app = drainUpdate(app, keyPress("", tea.KeyTab, tea.ModCtrl))

	if app.screen != ScreenSchema {
		t.Fatalf("screen = %v, want schema", app.screen)
	}
	view := app.screens[ScreenSchema].View(80, 20)
	if !containsPlain(view, "Connect first (screen 1)") {
		t.Fatalf("view missing connect-first message: %q", stripANSIForGolden(view))
	}
}

func containsPlain(s, substr string) bool {
	return len(stripANSIForGolden(s)) >= len(substr) &&
		indexPlain(stripANSIForGolden(s), substr) >= 0
}

func indexPlain(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
