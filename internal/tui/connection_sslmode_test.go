package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCycleSSLMode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "disable"},
		{"prefer", "disable"},
		{"disable", "require"},
		{"require", "verify-ca"},
		{"verify-ca", "verify-full"},
		{"verify-full", "disable"},
	}
	for _, tc := range cases {
		if got := cycleSSLMode(tc.in); got != tc.want {
			t.Fatalf("cycleSSLMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestConnectionSSLModeSpaceCyclesWithoutInsertingSpace(t *testing.T) {
	app := NewApp()
	cs := app.screens[ScreenConnection].(*connectionScreen)
	cs.panel = connPanelFields
	cs.nav.EnterInside(connSectionFields)
	cs.focus = 5 // SSLMODE
	before := app.conn.SSLMODE
	cs.Update(keyPress("", tea.KeySpace, 0))
	if app.conn.SSLMODE == before+" " {
		t.Fatalf("SSLMODE = %q, space was inserted", app.conn.SSLMODE)
	}
	if app.conn.SSLMODE != "disable" {
		t.Fatalf("SSLMODE = %q, want disable from empty", app.conn.SSLMODE)
	}
	cs.Update(keyPress("", tea.KeySpace, 0))
	if app.conn.SSLMODE != "require" {
		t.Fatalf("SSLMODE = %q, want require", app.conn.SSLMODE)
	}
}
