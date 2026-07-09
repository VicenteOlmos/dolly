package tui

import (
	"net/url"
	"testing"
)

func TestConnectionDraftDSNDefaultSecure(t *testing.T) {
	c := ConnectionDraft{
		Host:     "db.example.com",
		Database: "mydb",
		User:     "user",
		Password: "pass",
	}
	dsn := c.DSN()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("invalid DSN: %v", err)
	}
	q := u.Query()

	if q.Get("sslmode") != "verify-full" {
		t.Fatalf("sslmode = %q, want verify-full", q.Get("sslmode"))
	}
	if q.Get("channel_binding") != "require" {
		t.Fatalf("channel_binding = %q, want require", q.Get("channel_binding"))
	}
}

func TestConnectionDraftDSNSSLMODEDisableSkipsChannelBinding(t *testing.T) {
	c := ConnectionDraft{
		Host:     "db.example.com",
		Database: "mydb",
		User:     "user",
		Password: "pass",
		SSLMODE:  "disable",
	}
	dsn := c.DSN()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("invalid DSN: %v", err)
	}
	q := u.Query()

	if q.Get("sslmode") != "disable" {
		t.Fatalf("sslmode = %q, want disable", q.Get("sslmode"))
	}
	if q.Get("channel_binding") != "" {
		t.Fatalf("channel_binding = %q, want empty when sslmode=disable", q.Get("channel_binding"))
	}
}

func TestConnectionDraftDSNExplicitSSLMODERespected(t *testing.T) {
	c := ConnectionDraft{
		Host:     "db.example.com",
		Database: "mydb",
		User:     "user",
		Password: "pass",
		SSLMODE:  "require",
	}
	dsn := c.DSN()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("invalid DSN: %v", err)
	}
	q := u.Query()

	if q.Get("sslmode") != "require" {
		t.Fatalf("sslmode = %q, want require (explicit)", q.Get("sslmode"))
	}
}
