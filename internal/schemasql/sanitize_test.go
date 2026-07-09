package schemasql

import (
	"strings"
	"testing"
)

func TestSanitizeStripsUnsupportedSettings(t *testing.T) {
	in := []byte(strings.Join([]string{
		"SET statement_timeout = 0;",
		"SET transaction_timeout = 0;",
		"SET LOCAL transaction_timeout = 0;",
		"SET SESSION AUTHORIZATION DEFAULT;",
		"CREATE TABLE public.users (id integer);",
	}, "\n"))

	out, err := Sanitize(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "transaction_timeout") {
		t.Fatalf("schema still contains transaction_timeout:\n%s", got)
	}
	for _, want := range []string{"SET statement_timeout = 0;", "SET SESSION AUTHORIZATION DEFAULT;", "CREATE TABLE public.users"} {
		if !strings.Contains(got, want) {
			t.Fatalf("schema missing %q:\n%s", want, got)
		}
	}
}

func TestShouldStripSchemaSetLineIsNarrow(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"SET transaction_timeout = 0;", true},
		{"set TRANSACTION_TIMEOUT = '1s';", true},
		{"SET LOCAL transaction_timeout = 0;", true},
		{"SET statement_timeout = 0;", false},
		{"SET SESSION AUTHORIZATION DEFAULT;", false},
		{"-- SET transaction_timeout = 0;", false},
	}

	for _, tt := range cases {
		t.Run(tt.line, func(t *testing.T) {
			if got := shouldStripSchemaSetLine(tt.line); got != tt.want {
				t.Fatalf("shouldStripSchemaSetLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
