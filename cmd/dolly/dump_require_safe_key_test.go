package main

import "testing"

func TestParseDumpRequireSafeKey(t *testing.T) {
	flags, err := parseDumpFlags([]string{"--dsn", "postgres://u@h/db", "--require-safe-key"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.RequireSafeKey {
		t.Fatal("expected --require-safe-key")
	}
	if !dumpFlagsToOverrides(flags).RequireSafeKey {
		t.Fatal("expected RequireSafeKey override")
	}
}
