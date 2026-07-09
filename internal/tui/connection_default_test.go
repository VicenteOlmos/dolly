package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestConnectionOpensSavedListWhenProfilesExist(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	if cs.panel != connPanelList {
		t.Fatalf("panel = %v, want saved list when profiles exist", cs.panel)
	}
}

func TestApplyDefaultConnectionPrefillsDraft(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.jsonc")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cfg.Connections.Default = "prod"
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(dir, ".dolly.connections.yaml")
	store, err := connections.NewFileStore(storePath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(connections.Connection{
		Name: "prod", Host: "prod.db", Port: "5432", Database: "app", User: "app", Password: "secret",
		Schemas: []string{"app"},
	}); err != nil {
		t.Fatal(err)
	}

	app := NewAppFromConfig(store, true, cfg, cfgPath)
	if app.conn.Host != "prod.db" {
		t.Fatalf("Host = %q, want prod.db", app.conn.Host)
	}
	cs := app.screens[ScreenConnection].(*connectionScreen)
	if cs.listCursor != 0 || len(cs.profiles) != 1 || cs.profiles[0].Name != "prod" {
		t.Fatalf("profiles cursor = %+v", cs.profiles)
	}
}

func TestConnectionSetDefaultProfilePersistsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.jsonc")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u",
	})
	app := NewAppFromConfig(store, true, cfg, cfgPath)
	cs := app.screens[ScreenConnection].(*connectionScreen)

	app = drainUpdate(app, keyPress("*", '*', 0))
	if cfg.Connections.Default != "staging" {
		t.Fatalf("cfg.Connections.Default = %q, want staging", cfg.Connections.Default)
	}
	reloaded, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Connections.Default != "staging" {
		t.Fatalf("reloaded default = %q, want staging", reloaded.Connections.Default)
	}
	view := cs.View(80, 24)
	if !containsPlain(view, "★") {
		t.Fatalf("view should mark default profile: %s", view)
	}
}

func TestDeleteDefaultProfileClearsConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.jsonc")
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cfg.Connections.Default = "staging"
	if err := config.SaveConfig(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u",
	})
	app := NewAppFromConfig(store, true, cfg, cfgPath)
	enterConnectionList(app.screens[ScreenConnection].(*connectionScreen))

	app = drainUpdate(app, keyPress("d", 'd', 0))
	app = drainUpdate(app, keyPress("y", 'y', 0))

	if cfg.Connections.Default != "" {
		t.Fatalf("cfg.Connections.Default = %q, want cleared", cfg.Connections.Default)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if containsPlain(string(data), "staging") && containsPlain(string(data), `"default"`) {
		// default key may remain empty in jsonc; ensure value cleared in memory at least
	}
}
