package tui

import (
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestPrefillConnectionFullDraft(t *testing.T) {
	app := NewApp()
	draft := ConnectionDraft{
		Host:     "myhost",
		Port:     "5433",
		Database: "mydb",
		User:     "myuser",
		Password: "mypass",
	}
	app.PrefillConnection(draft)

	if app.conn.Host != "myhost" {
		t.Errorf("Host = %q, want %q", app.conn.Host, "myhost")
	}
	if app.conn.Port != "5433" {
		t.Errorf("Port = %q, want %q", app.conn.Port, "5433")
	}
	if app.conn.Database != "mydb" {
		t.Errorf("Database = %q, want %q", app.conn.Database, "mydb")
	}
	if app.conn.User != "myuser" {
		t.Errorf("User = %q, want %q", app.conn.User, "myuser")
	}
	if app.conn.Password != "mypass" {
		t.Errorf("Password = %q, want %q", app.conn.Password, "mypass")
	}
}

func TestPrefillConnectionPartialDraft(t *testing.T) {
	app := NewApp()
	app.conn = ConnectionDraft{
		Host:     "existinghost",
		Port:     "5432",
		Database: "existingdb",
		User:     "existinguser",
		Password: "existingpass",
	}

	partial := ConnectionDraft{
		Host: "newhost",
		User: "newuser",
	}
	app.PrefillConnection(partial)

	if app.conn.Host != "newhost" {
		t.Errorf("Host = %q, want %q", app.conn.Host, "newhost")
	}
	if app.conn.Port != "5432" {
		t.Errorf("Port should be unchanged, got %q", app.conn.Port)
	}
	if app.conn.Database != "existingdb" {
		t.Errorf("Database should be unchanged, got %q", app.conn.Database)
	}
	if app.conn.User != "newuser" {
		t.Errorf("User = %q, want %q", app.conn.User, "newuser")
	}
	if app.conn.Password != "existingpass" {
		t.Errorf("Password should be unchanged, got %q", app.conn.Password)
	}
}

func TestPrefillConnectionEmptyDraftLeavesConnZero(t *testing.T) {
	app := NewApp()
	app.PrefillConnection(ConnectionDraft{})

	if app.conn != (ConnectionDraft{}) {
		t.Errorf("conn should remain zero-value, got %+v", app.conn)
	}
}

func TestNewAppFromConfigPrePopulatesDumpOutputDir(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = "my_output_dir"
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.dump.OutputDir != "my_output_dir" {
		t.Fatalf("dump.OutputDir = %q, want %q", app.dump.OutputDir, "my_output_dir")
	}
}

func TestNewAppFromConfigEmptyDumpOutputDir(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Dump.OutputDir = ""
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.dump.OutputDir != "" {
		t.Fatalf("dump.OutputDir = %q, want empty when config is empty", app.dump.OutputDir)
	}
}

func TestNewAppFromConfigFollowsLiveCloneStrategy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.Strategy = "logical-stream"
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.clone.Strategy != "" {
		t.Fatalf("clone.Strategy = %q, want empty until the form overrides it", app.clone.Strategy)
	}
	if effectiveCloneStrategyForDraft(app.clone, cfg) != "logical-stream" {
		t.Fatalf("effective strategy = %q, want logical-stream", effectiveCloneStrategyForDraft(app.clone, cfg))
	}
	cfg.Clone.Strategy = "template"
	if effectiveCloneStrategyForDraft(app.clone, cfg) != "template" {
		t.Fatalf("effective strategy = %q, want template after config edit", effectiveCloneStrategyForDraft(app.clone, cfg))
	}
	app.clone.Strategy = "physical-backup"
	cfg.Clone.Strategy = "schema-replay"
	if effectiveCloneStrategyForDraft(app.clone, cfg) != "physical-backup" {
		t.Fatalf("effective strategy = %q, want the explicit form override", effectiveCloneStrategyForDraft(app.clone, cfg))
	}
}

func TestNewAppFromConfigEmptyCloneStrategyStaysEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.Strategy = ""
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.clone.Strategy != "" {
		t.Fatalf("clone.Strategy = %q, want empty", app.clone.Strategy)
	}
	if effectiveCloneStrategyForDraft(app.clone, cfg) != "schema-replay" {
		t.Fatalf("effective strategy = %q, want schema-replay fallback", effectiveCloneStrategyForDraft(app.clone, cfg))
	}
}

func TestNewAppFromConfigFollowsLiveCloneOnConflict(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.RestoreOnConflict = "skip"
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.clone.OnConflict != "" {
		t.Fatalf("clone.OnConflict = %q, want empty until the form overrides it", app.clone.OnConflict)
	}
	cs := app.screens[ScreenClone].(*cloneScreen)
	if cs.cloneOnConflictLabel() != "skip" {
		t.Fatalf("on-conflict label = %q, want skip from config", cs.cloneOnConflictLabel())
	}
	cfg.Clone.RestoreOnConflict = "upsert"
	if cs.cloneOnConflictLabel() != "upsert" {
		t.Fatalf("on-conflict label = %q, want upsert after config edit", cs.cloneOnConflictLabel())
	}
	app.clone.OnConflict = "error"
	cfg.Clone.RestoreOnConflict = "skip"
	if cs.cloneOnConflictLabel() != "error" {
		t.Fatalf("on-conflict label = %q, want the explicit form override", cs.cloneOnConflictLabel())
	}
}

func TestNewAppFromConfigEmptyCloneOnConflictStaysEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.RestoreOnConflict = ""
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	if app.clone.OnConflict != "" {
		t.Fatalf("clone.OnConflict = %q, want empty", app.clone.OnConflict)
	}
}

func TestNewAppFromConfigSectionEntryOverview(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TUI.SectionEntry = "overview"
	app := NewAppFromConfig(nil, false, cfg, "config.jsonc")
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if conn.panel != connPanelOverview {
		t.Fatalf("panel = %v, want overview when section_entry=overview", conn.panel)
	}
	if !conn.nav.InOverview() {
		t.Fatal("expected overview nav level")
	}
}

func TestConfigScreenHas28KnobsWithTUISection(t *testing.T) {
	fields := buildConfigFields()
	if len(fields) != 30 {
		t.Fatalf("buildConfigFields() returned %d fields, want 30", len(fields))
	}

	found := false
	for _, f := range fields {
		if f.Section == "dump" && f.Label == "output_dir" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("buildConfigFields() missing [dump] section with output_dir field")
	}

	foundTUI := false
	foundTheme := false
	for _, f := range fields {
		if f.Section == "tui" && f.Label == "section_entry" {
			foundTUI = true
		}
		if f.Section == "tui" && f.Label == "theme" {
			foundTheme = true
		}
	}
	if !foundTUI {
		t.Fatal("buildConfigFields() missing [tui] section_entry field")
	}
	if !foundTheme {
		t.Fatal("buildConfigFields() missing [tui] theme field")
	}
}

func TestPrefillConnectionDoesNotAutoConnect(t *testing.T) {
	app := NewApp()
	app.PrefillConnection(ConnectionDraft{
		Host:     "h",
		Port:     "5432",
		Database: "db",
		User:     "u",
		Password: "p",
	})

	if app.db != nil {
		t.Error("PrefillConnection must not open a DB connection")
	}
	if app.connStatus != ConnStatusIdle {
		t.Errorf("connStatus should remain idle, got %v", app.connStatus)
	}
}
