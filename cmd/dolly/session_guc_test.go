package main

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestApplySessionGUCsViaAppendQueryParam(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DB.LockTimeout = "10s"
	got, err := cfg.ApplySessionGUCs("postgres://user@localhost/db?sslmode=disable", appendQueryParam)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"application_name=dolly",
		"statement_timeout=5min",
		"lock_timeout=10000",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("ApplySessionGUCs() = %q, missing %q", got, part)
		}
	}
}

func TestApplySessionGUCsKeywordViaAppendQueryParam(t *testing.T) {
	cfg := config.DefaultConfig()
	got, err := cfg.ApplySessionGUCs("host=localhost port=5432", appendQueryParam)
	if err != nil {
		t.Fatal(err)
	}
	if got != "host=localhost port=5432 statement_timeout=5min application_name=dolly" {
		t.Fatalf("keyword inject = %q", got)
	}
}

func TestApplySessionGUCsKeywordTimeoutMilliseconds(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DB.StatementTimeout = "0"
	cfg.DB.LockTimeout = "1m"
	cfg.DB.IdleInTransactionSessionTimeout = "1m30s"
	cfg.DB.ApplicationName = ""

	got, err := cfg.ApplySessionGUCs("host=localhost dbname=mydb", appendQueryParam)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"lock_timeout=60000",
		"idle_in_transaction_session_timeout=90000",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("ApplySessionGUCs() = %q, missing %q", got, part)
		}
	}
	for _, bad := range []string{"lock_timeout=1m", "idle_in_transaction_session_timeout=1m30s"} {
		if strings.Contains(got, bad) {
			t.Fatalf("ApplySessionGUCs() = %q, must not contain Go duration %q", got, bad)
		}
	}
}

func TestApplySessionGUCsKeywordApplicationNameQuoting(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DB.StatementTimeout = "0"
	cfg.DB.LockTimeout = ""
	cfg.DB.IdleInTransactionSessionTimeout = ""
	cfg.DB.ApplicationName = "dolly worker"

	got, err := cfg.ApplySessionGUCs("host=localhost dbname=mydb", appendQueryParam)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "application_name='dolly worker'") {
		t.Fatalf("ApplySessionGUCs() = %q, want quoted application_name", got)
	}
}
