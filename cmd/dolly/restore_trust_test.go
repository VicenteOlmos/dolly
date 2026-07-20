package main

import "testing"

func TestRestoreTrustSchemaSQLIsExplicit(t *testing.T) {
	base := []string{"--dsn", "postgres://localhost/db", "--input", "dump"}
	flags, err := parseRestoreFlags(base)
	if err != nil || flags.TrustSchemaSQL {
		t.Fatalf("default flags=%+v err=%v", flags, err)
	}
	flags, err = parseRestoreFlags(append(base, "--trust-schema-sql"))
	if err != nil || !flags.TrustSchemaSQL {
		t.Fatalf("trusted flags=%+v err=%v", flags, err)
	}
}
