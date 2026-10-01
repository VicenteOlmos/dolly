package tui

import "testing"

func TestConnectionFieldHelpDocumentsTLSFields(t *testing.T) {
	cases := map[string]string{
		"SSLMODE":         "Space",
		"Channel binding": "channel binding",
		"SSL root cert":   "sslrootcert",
		"SSL cert":        "sslcert",
		"SSL key":         "sslkey",
	}
	for label, want := range cases {
		help := connectionFieldHelp(label, 80)
		if help == "" || !containsPlain(help, want) {
			t.Fatalf("help for %q = %q, want substring %q", label, help, want)
		}
	}
}
