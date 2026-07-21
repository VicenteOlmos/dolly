package main

import (
	"strings"
	"testing"
)

func TestRestoreTrustSchemaSQLIsExplicit(t *testing.T) {
	base := []string{"--dsn", "postgres://localhost/db", "--input", "dump"}
	flags, err := parseRestoreFlags(base)
	if err != nil || flags.TrustSchemaSQL {
		t.Fatalf("default flags=%+v err=%v", flags, err)
	}

	_, err = parseRestoreFlags(append(base, "--trust-schema-sql"))
	if err == nil || !strings.Contains(err.Error(), "--no-transaction") {
		t.Fatalf("trust without no-transaction err = %v", err)
	}

	_, err = parseRestoreFlags(append(base, "--trust-schema-sql", "--no-transaction"))
	if err == nil || !strings.Contains(err.Error(), "--yes to confirm") {
		t.Fatalf("trust without yes err = %v", err)
	}

	flags, err = parseRestoreFlags(append(base, "--trust-schema-sql", "--no-transaction", "--yes"))
	if err != nil || !flags.TrustSchemaSQL || !flags.NoTransaction || !flags.Yes {
		t.Fatalf("trusted flags=%+v err=%v", flags, err)
	}
}
