package tui

import (
	"database/sql"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestFieldCursorInsertAtMiddle(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection
	conn := app.screens[ScreenConnection].(*connectionScreen)
	conn.focus = 0
	app.conn.Host = "abc"
	conn.fieldCursors[0] = 3

	app = drainUpdate(app, keyPress("", tea.KeyLeft, 0))
	app = drainUpdate(app, keyPress("", tea.KeyLeft, 0))
	app = drainUpdate(app, keyPress("X", 'X', 0))

	if app.conn.Host != "aXbc" {
		t.Fatalf("Host = %q, want aXbc", app.conn.Host)
	}
}

func TestFieldCursorLeftRightHomeEnd(t *testing.T) {
	var value string
	cursor := 0
	value = "hello"
	cursor = 5

	fieldCursorLeft(&cursor)
	if cursor != 4 {
		t.Fatalf("cursor after left = %d, want 4", cursor)
	}
	fieldCursorRight(&cursor, len(value))
	if cursor != 5 {
		t.Fatalf("cursor after right = %d, want 5", cursor)
	}
	fieldCursorHome(&cursor)
	if cursor != 0 {
		t.Fatalf("cursor after home = %d, want 0", cursor)
	}
	fieldCursorEnd(&cursor, len(value))
	if cursor != 5 {
		t.Fatalf("cursor after end = %d, want 5", cursor)
	}
}

func TestMaskDSNPassword(t *testing.T) {
	got := maskDSNPassword("postgres://app:secret@db.example.com:5432/app?sslmode=disable")
	want := "postgres://app:********@db.example.com:5432/app?sslmode=disable"
	if got != want {
		t.Fatalf("maskDSNPassword = %q, want %q", got, want)
	}
	got = maskDSNPassword("postgres://app@db.example.com/app?password=secret&sslmode=disable")
	if strings.Contains(got, "secret") || !strings.Contains(got, "password=********") {
		t.Fatalf("maskDSNPassword query leak = %q", got)
	}
	if maskDSNPassword("") != "" {
		t.Fatal("expected empty DSN to stay empty")
	}
}

func TestPasswordMaskFixedLength(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: ""},
		{name: "one", value: "x", want: "********"},
		{name: "long", value: "abcdefghijklmnopqrst", want: "********"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maskedPasswordDisplay(tc.value)
			if got != tc.want {
				t.Fatalf("maskedPasswordDisplay(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestConnectionPasswordRendersEightStarsWhenEditing(t *testing.T) {
	prof := connections.Connection{
		Name: "staging", Host: "h", Port: "5432", Database: "d", User: "u", Password: "x",
	}
	store := newMockConnectionStore(prof)
	app := NewAppWithOptions(mockSchemaLoader{}, mockDumpRunner{}, nil, nil, nil, store, true)
	app.screen = ScreenConnection
	cs := app.screens[ScreenConnection].(*connectionScreen)
	enterConnectionList(cs)
	_ = cs.Update(keyPress("e", 'e', 0))

	view := stripANSIForGolden(app.View().Content)
	if !containsPlain(view, "Password: ********") {
		t.Fatalf("view missing fixed password mask:\n%s", view)
	}
}

func TestCursorKeysDoNotChangeScreen(t *testing.T) {
	app := NewApp()
	app.screen = ScreenConnection

	app = drainUpdate(app, keyPress("", tea.KeyRight, 0))
	if app.screen != ScreenConnection {
		t.Fatalf("screen = %v, want connection after right arrow in field", app.screen)
	}
}

func TestDumpPathCursorInsert(t *testing.T) {
	app := NewApp()
	conn, err := sql.Open("pgx", "postgres://u:p@h-x/db_stub")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	app.db = conn
	app.screen = ScreenDump
	ds := app.screens[ScreenDump].(*dumpScreen)
	enterDumpSection(ds, dumpSectionPath)
	app.dump.OutputDir = "ac"
	ds.pathCursor = 1

	app = drainUpdate(app, keyPress("b", 'b', 0))
	if app.dump.OutputDir != "abc" {
		t.Fatalf("OutputDir = %q, want abc", app.dump.OutputDir)
	}
}
