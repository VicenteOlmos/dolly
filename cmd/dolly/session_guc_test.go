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
		"lock_timeout=10s",
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
