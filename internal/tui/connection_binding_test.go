package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCycleChannelBinding(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "require"},
		{"bogus", "require"},
		{"require", "prefer"},
		{"prefer", "disable"},
		{"disable", "require"},
	}
	for _, tt := range tests {
		if got := cycleChannelBinding(tt.in); got != tt.want {
			t.Fatalf("cycleChannelBinding(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConnectionScreenChannelBindingSpaceCycles(t *testing.T) {
	draft := ConnectionDraft{
		Host:     "h",
		Port:     "5432",
		Database: "db",
		User:     "u",
		Password: "p",
		SSLMODE:  "require",
	}
	status := ConnStatusIdle
	var errMsg string
	screen := newConnectionScreen(&draft, &status, &errMsg, nil, false, nil, nil, nil, SectionEntryInside)
	cs := screen.(*connectionScreen)
	enterConnectionFields(cs)
	cs.focus = 6

	screen.Update(keyPress("", tea.KeySpace, 0))
	if draft.ChannelBinding != "require" {
		t.Fatalf("ChannelBinding = %q, want require after first Space from empty", draft.ChannelBinding)
	}
	screen.Update(keyPress("", tea.KeySpace, 0))
	if draft.ChannelBinding != "prefer" {
		t.Fatalf("ChannelBinding = %q, want prefer", draft.ChannelBinding)
	}

	q := mustParseDSNQuery(draft.DSN())
	if q.Get("channel_binding") != "prefer" {
		t.Fatalf("DSN channel_binding = %q, want prefer", q.Get("channel_binding"))
	}
}
