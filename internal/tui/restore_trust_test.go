package tui

import "testing"

func TestRestoreHelpDescribesExplicitTrust(t *testing.T) {
	for _, flag := range catalogCommand("restore").Flags {
		if flag.Name == "trust-schema-sql" && flag.Default == "" {
			return
		}
	}
	t.Fatal("restore help must expose default-off trust-schema-sql")
}
