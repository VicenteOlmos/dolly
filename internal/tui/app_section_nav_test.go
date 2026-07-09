package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestFKeyNoOpOnOverview(t *testing.T) {
	t.Run("dump", func(t *testing.T) {
		app := NewApp()
		app.screen = ScreenDump
		app.dumpStatus = DumpStatusIdle
		ds := app.screens[ScreenDump].(*dumpScreen)
		before := ds.nav

		app = drainUpdate(app, keyPress("f", 'f', 0))
		ds = app.screens[ScreenDump].(*dumpScreen)
		if app.screen != ScreenDump {
			t.Fatalf("screen = %v, want dump", app.screen)
		}
		if ds.nav != before {
			t.Fatalf("nav = %+v, want unchanged %+v", ds.nav, before)
		}
	})

	t.Run("clone", func(t *testing.T) {
		app := NewApp()
		app.screen = ScreenClone
		app.cloneStatus = CloneStatusIdle
		cs := app.screens[ScreenClone].(*cloneScreen)
		before := cs.nav

		app = drainUpdate(app, keyPress("f", 'f', 0))
		cs = app.screens[ScreenClone].(*cloneScreen)
		if app.screen != ScreenClone {
			t.Fatalf("screen = %v, want clone", app.screen)
		}
		if cs.nav != before {
			t.Fatalf("nav = %+v, want unchanged %+v", cs.nav, before)
		}
	})

	t.Run("connection overview", func(t *testing.T) {
		store := newMockConnectionStore(connections.Connection{
			Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
		})
		app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
		app.screen = ScreenConnection
		conn := app.screens[ScreenConnection].(*connectionScreen)
		beforePanel := conn.panel
		beforeNav := conn.nav

		app = drainUpdate(app, keyPress("f", 'f', 0))
		conn = app.screens[ScreenConnection].(*connectionScreen)
		if app.screen != ScreenConnection {
			t.Fatalf("screen = %v, want connection", app.screen)
		}
		if conn.panel != beforePanel {
			t.Fatalf("panel = %v, want %v", conn.panel, beforePanel)
		}
		if conn.nav != beforeNav {
			t.Fatalf("nav = %+v, want unchanged %+v", conn.nav, beforeNav)
		}
	})

	t.Run("connection saved list", func(t *testing.T) {
		store := newMockConnectionStore(connections.Connection{
			Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app",
		})
		app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
		app.screen = ScreenConnection
		conn := app.screens[ScreenConnection].(*connectionScreen)
		enterConnectionList(conn)
		beforePanel := conn.panel
		beforeCursor := conn.listCursor

		app = drainUpdate(app, keyPress("f", 'f', 0))
		conn = app.screens[ScreenConnection].(*connectionScreen)
		if app.screen != ScreenConnection {
			t.Fatalf("screen = %v, want connection", app.screen)
		}
		if conn.panel != beforePanel {
			t.Fatalf("panel = %v, want %v", conn.panel, beforePanel)
		}
		if conn.listCursor != beforeCursor {
			t.Fatalf("listCursor = %d, want unchanged %d", conn.listCursor, beforeCursor)
		}
	})
}

func TestAppDumpOverviewSectionsCycle(t *testing.T) {
	app := NewApp()
	app.screen = ScreenDump
	app.dumpStatus = DumpStatusIdle

	ds := app.screens[ScreenDump].(*dumpScreen)
	if ds.nav.Section != dumpSectionPath {
		t.Fatalf("initial section = %d, want path", ds.nav.Section)
	}

	want := []int{dumpSectionPicker, dumpSectionHistory, dumpSectionLog, dumpSectionPath}
	for i, section := range want {
		app = drainUpdate(app, keyPress("", tea.KeyDown, 0))
		ds = app.screens[ScreenDump].(*dumpScreen)
		if ds.nav.Section != section {
			t.Fatalf("after ↓ #%d: section = %d, want %d", i+1, ds.nav.Section, section)
		}
		if !ds.nav.InOverview() {
			t.Fatal("expected overview while cycling sections")
		}
	}

	app = drainUpdate(app, keyPress("", tea.KeyEnter, 0))
	if app.dumpStatus != DumpStatusIdle {
		t.Fatalf("dumpStatus = %v, want idle after Enter on overview", app.dumpStatus)
	}
}
