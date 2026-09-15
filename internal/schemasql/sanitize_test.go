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

func TestSanitizeReaderRewritesCreateSchemaIfNotExists(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader("CREATE SCHEMA public;\nCREATE TABLE public.t (id integer);\n")
	if err := SanitizeReader(in, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "CREATE SCHEMA IF NOT EXISTS public;") {
		t.Fatalf("reader output missing IF NOT EXISTS:\n%s", got)
	}
	if strings.Contains(got, "CREATE SCHEMA public;") {
		t.Fatalf("bare CREATE SCHEMA public remained:\n%s", got)
	}
}

func TestSanitizeRewritesCreateSchemaIfNotExists(t *testing.T) {
	in := []byte(strings.Join([]string{
		"-- Name: public; Type: SCHEMA",
		"CREATE SCHEMA public;",
		"CREATE SCHEMA IF NOT EXISTS app;",
		`CREATE SCHEMA "Billing";`,
		"CREATE SCHEMA public AUTHORIZATION postgres;",
		"CREATE TABLE public.users (id integer);",
	}, "\n"))

	out, err := Sanitize(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"CREATE SCHEMA IF NOT EXISTS public;",
		"CREATE SCHEMA IF NOT EXISTS app;",
		`CREATE SCHEMA IF NOT EXISTS "Billing";`,
		"CREATE SCHEMA IF NOT EXISTS public AUTHORIZATION postgres;",
		"CREATE TABLE public.users (id integer);",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("schema missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CREATE SCHEMA public;") {
		t.Fatalf("bare CREATE SCHEMA public remained:\n%s", got)
	}
	if strings.Count(got, "IF NOT EXISTS") != 4 {
		t.Fatalf("unexpected IF NOT EXISTS count in:\n%s", got)
	}
}

func TestSanitizeSkipsCreateSchemaInsideQuotesAndComments(t *testing.T) {
	in := []byte(strings.Join([]string{
		"CREATE SCHEMA public;",
		"CREATE FUNCTION public.touch_schema() RETURNS void",
		"    LANGUAGE sql",
		"    AS $$",
		"CREATE SCHEMA interior_dollar;",
		"    CREATE SCHEMA interior_indented;",
		"$$;",
		"CREATE FUNCTION public.touch_tagged() RETURNS void",
		"    LANGUAGE plpgsql",
		"    AS $function$",
		"BEGIN",
		"CREATE SCHEMA interior_tagged;",
		"END;",
		"$function$;",
		"SELECT '",
		"CREATE SCHEMA interior_string;",
		"';",
		"/*",
		"CREATE SCHEMA interior_block;",
		"  /* nested */",
		"CREATE SCHEMA interior_nested;",
		"*/",
		"CREATE SCHEMA app;",
	}, "\n"))

	out, err := Sanitize(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"CREATE SCHEMA IF NOT EXISTS public;",
		"CREATE SCHEMA interior_dollar;",
		"    CREATE SCHEMA interior_indented;",
		"CREATE SCHEMA interior_tagged;",
		"CREATE SCHEMA interior_string;",
		"CREATE SCHEMA interior_block;",
		"CREATE SCHEMA interior_nested;",
		"CREATE SCHEMA IF NOT EXISTS app;",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("schema missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CREATE SCHEMA IF NOT EXISTS interior_") {
		t.Fatalf("rewrote CREATE SCHEMA inside quotes or comments:\n%s", got)
	}
	if strings.Count(got, "IF NOT EXISTS") != 2 {
		t.Fatalf("unexpected IF NOT EXISTS count in:\n%s", got)
	}
}

func TestSanitizeReaderSkipsCreateSchemaInsideDollarQuotes(t *testing.T) {
	var out strings.Builder
	in := strings.NewReader(strings.Join([]string{
		"CREATE SCHEMA public;",
		"AS $$",
		"CREATE SCHEMA interior;",
		"$$;",
	}, "\n"))
	if err := SanitizeReader(in, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "CREATE SCHEMA IF NOT EXISTS public;") {
		t.Fatalf("top-level CREATE SCHEMA not rewritten:\n%s", got)
	}
	if !strings.Contains(got, "CREATE SCHEMA interior;") {
		t.Fatalf("dollar-quoted CREATE SCHEMA was rewritten:\n%s", got)
	}
	if strings.Contains(got, "CREATE SCHEMA IF NOT EXISTS interior;") {
		t.Fatalf("rewrote CREATE SCHEMA inside dollar quotes:\n%s", got)
	}
}

func TestRewriteCreateSchemaIfNotExistsIsNarrow(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"CREATE SCHEMA public;", "CREATE SCHEMA IF NOT EXISTS public;"},
		{"create schema public;", "create schema IF NOT EXISTS public;"},
		{"\tCREATE SCHEMA public;", "\tCREATE SCHEMA IF NOT EXISTS public;"},
		{"CREATE SCHEMA IF NOT EXISTS public;", "CREATE SCHEMA IF NOT EXISTS public;"},
		{"CREATE SCHEMA  IF NOT EXISTS public;", "CREATE SCHEMA  IF NOT EXISTS public;"},
		{"-- CREATE SCHEMA public;", "-- CREATE SCHEMA public;"},
		{"CREATE TABLE public.users (id integer);", "CREATE TABLE public.users (id integer);"},
		{"CREATE SCHEMA AUTHORIZATION current_user;", "CREATE SCHEMA IF NOT EXISTS AUTHORIZATION current_user;"},
	}

	for _, tt := range cases {
		t.Run(tt.line, func(t *testing.T) {
			if got := rewriteCreateSchemaIfNotExists(tt.line); got != tt.want {
				t.Fatalf("rewriteCreateSchemaIfNotExists(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
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
