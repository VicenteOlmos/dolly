package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestConnectionPlainTabCyclesFieldNotScreen(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)

	app = drainUpdate(app, keyPress("", tea.KeyTab, 0))
	if app.screen != ScreenConnection {
		t.Fatalf("screen after Tab = %v, want connection", app.screen)
	}
	conn = app.screens[ScreenConnection].(*connectionScreen)
	if conn.focus != 1 {
		t.Fatalf("focus after tab = %d, want 1", conn.focus)
	}
}

func TestConnectionCtrlTabAdvancesScreenWhenSaveConnectionsDisabled(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, false)
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", tea.KeyTab, tea.ModCtrl))
	if app.screen != ScreenSchema {
		t.Fatalf("screen after Ctrl+Tab = %v, want schema when save_connections is false", app.screen)
	}
}

func TestConnectionArrowMovesFieldFocusWithoutSavedProfiles(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if conn.focus != 1 {
		t.Fatalf("focus after down = %d, want 1", conn.focus)
	}
}

func TestConnectionEscFromFieldsReturnsToOverview(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if !conn.nav.InInside() {
		t.Fatal("expected inside fields on default entry")
	}

	app = drainUpdate(app, keyPress("", tea.KeyEscape, 0))
	conn = app.screens[ScreenConnection].(*connectionScreen)
	if conn.panel != connPanelOverview {
		t.Fatalf("panel = %v, want overview after Esc", conn.panel)
	}
	if !conn.nav.InOverview() {
		t.Fatal("expected overview nav after Esc")
	}
}

func TestConnectionOverviewShowsSavedProfilesList(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.sectionEntry = SectionEntryOverview
	app.applyScreenSectionEntry(ScreenConnection)
	app.width = 80
	app.height = 24
	cs := app.screens[ScreenConnection].(*connectionScreen)
	if cs.panel != connPanelOverview {
		t.Fatalf("panel = %v, want overview", cs.panel)
	}

	view := stripANSIForGolden(cs.View(80, 24))
	if !containsPlain(view, "staging") {
		t.Fatalf("overview should list saved profile names, got:\n%s", view)
	}
}

func TestConnectionOverviewDrillSections(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.sectionEntry = SectionEntryOverview
	app.applyScreenSectionEntry(ScreenConnection)
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if conn.panel != connPanelOverview {
		t.Fatalf("initial panel = %v, want overview", conn.panel)
	}

	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	conn = app.screens[ScreenConnection].(*connectionScreen)
	if conn.nav.Section != connSectionList {
		t.Fatalf("section after down = %d, want saved list", conn.nav.Section)
	}

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	conn = app.screens[ScreenConnection].(*connectionScreen)
	if conn.panel != connPanelList {
		t.Fatalf("panel after Enter = %v, want list", conn.panel)
	}

	app = drainUpdate(app, keyPress("", tea.KeyEscape, 0))
	conn = app.screens[ScreenConnection].(*connectionScreen)
	if conn.panel != connPanelOverview {
		t.Fatalf("panel after Esc = %v, want overview", conn.panel)
	}
}

func TestConnectionArrowMovesSectionInOverview(t *testing.T) {
	store := newMockConnectionStore()
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.sectionEntry = SectionEntryOverview
	app.applyScreenSectionEntry(ScreenConnection)
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
	conn := app.screens[ScreenConnection].(*connectionScreen)
	if conn.nav.Section != connSectionList {
		t.Fatalf("section after down = %d, want list section in overview", conn.nav.Section)
	}
}

func TestConnectionTypingTDoesNotTest(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("t", 't', 0))
	if app.connStatus != ConnStatusIdle {
		t.Fatalf("connStatus = %v, want idle (plain t must type)", app.connStatus)
	}
	if app.conn.Host != "t" {
		t.Fatalf("Host = %q, want t", app.conn.Host)
	}
}

func TestConnectionCtrlTTestsFromFields(t *testing.T) {
	app := NewAppWithLoader(mockSchemaLoader{})
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", 't', tea.ModCtrl))
	if !containsPlain(app.statusMsg, "Connection OK") {
		t.Fatalf("statusMsg = %q, want Connection OK after Ctrl+t", app.statusMsg)
	}
}

func TestConnectionListPanelLetterTests(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("t", 't', 0))
	if !containsPlain(app.statusMsg, "Connection OK") {
		t.Fatalf("statusMsg = %q, want Connection OK after t on list panel", app.statusMsg)
	}
}

func TestConnectionListPanelFunctionKeysIgnored(t *testing.T) {
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u",
	})
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)

	for _, code := range []rune{tea.KeyF2, tea.KeyF3, tea.KeyF4, tea.KeyF5, tea.KeyF9} {
		before := cs.panel
		_ = cs.Update(keyPress("", code, 0))
		if cs.panel != before {
			t.Fatalf("F-key %v changed panel from %v to %v", code, before, cs.panel)
		}
	}
	if len(cs.profiles) != 1 || cs.profiles[0].Name != "staging" {
		t.Fatalf("profiles = %+v, want unchanged staging", cs.profiles)
	}
}

func TestConnectionHostIPDigitsDoNotChangeScreen(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionFields(conn)
	conn.focus = 0 // Host

	for _, ch := range "192.168.1.5" {
		app = drainUpdate(app, keyPress(string(ch), rune(ch), 0))
	}
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection after typing host IP", app.screen)
	}
	if app.conn.Host != "192.168.1.5" {
		t.Fatalf("Host = %q, want 192.168.1.5", app.conn.Host)
	}
}

func TestConnectionPortDigitsDoNotChangeScreen(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionFields(conn)
	conn.focus = 1 // Port

	for _, ch := range "5432" {
		app = drainUpdate(app, keyPress(string(ch), rune(ch), 0))
	}
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection after typing port", app.screen)
	}
	if app.conn.Port != "5432" {
		t.Fatalf("Port = %q, want 5432", app.conn.Port)
	}
}
