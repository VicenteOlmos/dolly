package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/connections"
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
	screen := newConnectionScreen(&draft, &status, &errMsg, nil, false, nil, nil, nil, SectionEntryInside, nil)
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

func TestChannelBindingRoundTripsThroughSavedProfile(t *testing.T) {
	draft := ConnectionDraft{
		Host:           "h",
		Port:           "5432",
		Database:       "db",
		User:           "u",
		Password:       "p",
		SSLMODE:        "verify-full",
		ChannelBinding: "disable",
	}
	saved := connectionFromDraft(draft, "local", []string{"public"})
	if saved.ChannelBinding != "disable" {
		t.Fatalf("saved binding = %q, want disable", saved.ChannelBinding)
	}
	again := draftFromConnection(saved)
	if again.ChannelBinding != "disable" {
		t.Fatalf("draft binding = %q, want disable", again.ChannelBinding)
	}
	if got := saved.DSN(); !containsPlain(got, "channel_binding=disable") {
		t.Fatalf("profile DSN = %q, want disable", got)
	}
	legacy := connections.Connection{Host: "h", Database: "db", User: "u", Password: "p"}
	if got := legacy.DSN(); !containsPlain(got, "channel_binding=require") {
		t.Fatalf("legacy DSN = %q, want require", got)
	}
}

func TestTLSFilesRoundTripThroughSavedProfile(t *testing.T) {
	draft := ConnectionDraft{
		Host: "h", Port: "5432", Database: "db", User: "u", Password: "p",
		SSLMODE: "verify-full", SSLRootCert: "/certs/root.crt", SSLCert: "/certs/client.crt", SSLKey: "/certs/client.key",
	}
	saved := connectionFromDraft(draft, "local", nil)
	again := draftFromConnection(saved)
	if again.SSLRootCert != draft.SSLRootCert || again.SSLCert != draft.SSLCert || again.SSLKey != draft.SSLKey {
		t.Fatalf("round trip = %+v", again)
	}
	if got := saved.DSN(); !containsPlain(got, "sslrootcert=") || !containsPlain(got, "sslkey=") {
		t.Fatalf("profile DSN = %q, want TLS files", got)
	}
	if got := draft.DSN(); !containsPlain(got, "sslcert=") {
		t.Fatalf("draft DSN = %q, want sslcert", got)
	}
	draft.SSLMODE = "disable"
	if got := draft.DSN(); containsPlain(got, "sslrootcert=") {
		t.Fatalf("draft DSN = %q, want TLS files omitted", got)
	}
}

func TestConnectFromDraftUsesEditedTLSFiles(t *testing.T) {
	saved := connections.Connection{
		Name: "local", Host: "h", Port: "5432", Database: "db", User: "u", Password: "p",
		SSLMODE: "verify-full", Schemas: []string{"app"},
		SSLRootCert: "/certs/old-root.crt", SSLCert: "/certs/client.crt", SSLKey: "/certs/client.key",
	}
	store := newMockConnectionStore(saved)
	draft := ConnectionDraft{
		Host: "h", Port: "5432", Database: "db", User: "u", Password: "p",
		SSLMODE:     "verify-full",
		SSLRootCert: "/certs/old-root.crt", SSLCert: "/certs/client.crt", SSLKey: "/certs/client.key",
	}
	status := ConnStatusIdle
	var errMsg string
	screen := newConnectionScreen(&draft, &status, &errMsg, store, false, nil, nil, nil, SectionEntryInside, nil)
	cs := screen.(*connectionScreen)
	enterConnectionFields(cs)

	msg := cs.connectFromDraft()().(connectRequestedMsg)
	if got := mustParseDSNQuery(msg.dsn).Get("sslrootcert"); got != "/certs/old-root.crt" {
		t.Fatalf("unchanged sslrootcert = %q", got)
	}
	if len(msg.schemas) != 1 || msg.schemas[0] != "app" {
		t.Fatalf("schemas = %v, want [app]", msg.schemas)
	}

	draft.SSLRootCert = "/certs/new-root.crt"
	msg = cs.connectFromDraft()().(connectRequestedMsg)
	if got := mustParseDSNQuery(msg.dsn).Get("sslrootcert"); got != "/certs/new-root.crt" {
		t.Fatalf("edited sslrootcert = %q", got)
	}
	if draft.SSLRootCert != "/certs/new-root.crt" {
		t.Fatalf("draft lost edited cert: %q", draft.SSLRootCert)
	}
	if len(msg.schemas) != 1 || msg.schemas[0] != "app" {
		t.Fatalf("edited connect schemas = %v", msg.schemas)
	}

	enterConnectionList(cs)
	cs.listCursor = 0
	cmd := cs.Update(keyPress("", tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("expected connect from saved list")
	}
	msg = cmd().(connectRequestedMsg)
	if got := mustParseDSNQuery(msg.dsn).Get("sslrootcert"); got != "/certs/old-root.crt" {
		t.Fatalf("saved list sslrootcert = %q", got)
	}
}
