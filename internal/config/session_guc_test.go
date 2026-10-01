package config

import (
	"strings"
	"testing"
)

func TestApplySessionGUCsDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB.LockTimeout = "30s"
	cfg.DB.IdleInTransactionSessionTimeout = "1m30s"

	var keys []string
	set := func(dsn, key, value string) (string, error) {
		keys = append(keys, key)
		return dsn + " " + key + "=" + value, nil
	}
	got, err := cfg.ApplySessionGUCs("dsn", set)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"statement_timeout",
		"lock_timeout",
		"idle_in_transaction_session_timeout",
		"application_name",
	}
	if len(keys) != len(wantKeys) {
		t.Fatalf("setter keys = %v, want %v", keys, wantKeys)
	}
	for i, k := range wantKeys {
		if keys[i] != k {
			t.Fatalf("keys[%d] = %q, want %q", i, keys[i], k)
		}
	}
	for _, part := range []string{
		"application_name=dolly",
		"statement_timeout=5min",
		"lock_timeout=30000",
		"idle_in_transaction_session_timeout=90000",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("ApplySessionGUCs() = %q, missing %q", got, part)
		}
	}
}

func TestApplySessionGUCsSkipsDisabledTimeouts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DB.StatementTimeout = "0"
	cfg.DB.LockTimeout = ""
	cfg.DB.IdleInTransactionSessionTimeout = "0"
	cfg.DB.ApplicationName = ""

	called := false
	set := func(dsn, key, value string) (string, error) {
		called = true
		return dsn, nil
	}
	got, err := cfg.ApplySessionGUCs("dsn", set)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("setter should not run when all session params disabled")
	}
	if got != "dsn" {
		t.Fatalf("ApplySessionGUCs() = %q, want unchanged DSN", got)
	}
}

func TestSessionGUCTimeoutForDSN(t *testing.T) {
	tests := map[string]string{
		"1m":     "60000",
		"1m30s":  "90000",
		"5s":     "5000",
		"5min":   "5min",
		"30s":    "30000",
		"250ms":  "250",
	}
	for in, want := range tests {
		if got := sessionGUCTimeoutForDSN(in); got != want {
			t.Fatalf("sessionGUCTimeoutForDSN(%q) = %q, want %q", in, got, want)
		}
	}
}
