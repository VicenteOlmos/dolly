package connections

import (
	"strings"
	"testing"
)

func TestSubprocessDSNRemovesAllPasswords(t *testing.T) {
	const secret = "p%/^=word"
	clean, password, err := SubprocessDSN("postgres://user:" + secret + "@localhost/db?password=query-secret&sslmode=disable&statement_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	if password != secret {
		t.Fatalf("password = %q", password)
	}
	for _, leaked := range []string{secret, "query-secret", "password=", "statement_timeout"} {
		if strings.Contains(clean, leaked) {
			t.Fatalf("clean DSN leaked %q: %q", leaked, clean)
		}
	}
}

func TestSubprocessDSNNilUserinfoAndMalformedFailClosed(t *testing.T) {
	clean, password, err := SubprocessDSN("postgres://localhost/db?password=query-secret")
	if err != nil || password != "query-secret" || strings.Contains(clean, "query-secret") {
		t.Fatalf("clean=%q password=%q err=%v", clean, password, err)
	}
	if _, _, err := SubprocessDSN("not a postgres dsn with secret=never-leak"); err == nil || strings.Contains(err.Error(), "never-leak") {
		t.Fatalf("malformed DSN error = %v", err)
	}
}

func TestSubprocessDSNRemovesUnsupportedCredentialParameters(t *testing.T) {
	clean, password, err := SubprocessDSN("postgres://user:password@localhost/db?sslmode=verify-full&channel_binding=require&sslpassword=tls-secret")
	if err != nil {
		t.Fatal(err)
	}
	if password != "password" {
		t.Fatalf("password = %q", password)
	}
	for _, leaked := range []string{"password", "tls-secret", "sslpassword"} {
		if strings.Contains(clean, leaked) {
			t.Fatalf("clean DSN leaked %q: %q", leaked, clean)
		}
	}
	for _, required := range []string{"sslmode=verify-full", "channel_binding=require"} {
		if !strings.Contains(clean, required) {
			t.Fatalf("clean DSN lost %q: %q", required, clean)
		}
	}
}

func TestSubprocessDSNAllowsOnlyKnownNonSecretQueryControls(t *testing.T) {
	clean, _, err := SubprocessDSN("postgres://user:password@localhost/db?sslmode=verify-full&channel_binding=require&sslcert=%2Fcert.pem&sslkey=%2Fkey.pem&token=token-secret&pass=pass-secret&pwd=pwd-secret&custom_password=password-secret&custom_secret=secret-value&custom_token=custom-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"token-secret", "pass-secret", "pwd-secret", "password-secret", "secret-value", "custom-token"} {
		if strings.Contains(clean, leaked) {
			t.Fatalf("clean DSN leaked %q: %q", leaked, clean)
		}
	}
	for _, required := range []string{"sslmode=verify-full", "channel_binding=require", "sslcert=%2Fcert.pem", "sslkey=%2Fkey.pem"} {
		if !strings.Contains(clean, required) {
			t.Fatalf("clean DSN lost %q: %q", required, clean)
		}
	}
}

func TestSubprocessDSNPreservesLibpqLocationParameters(t *testing.T) {
	clean, password, err := SubprocessDSN("postgresql:///mydb?host=/var/lib/postgresql&hostaddr=127.0.0.1&port=5433&sslmode=disable&password=query-secret&statement_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	if password != "query-secret" {
		t.Fatalf("password = %q", password)
	}
	for _, leaked := range []string{"query-secret", "password=", "statement_timeout"} {
		if strings.Contains(clean, leaked) {
			t.Fatalf("clean DSN leaked %q: %q", leaked, clean)
		}
	}
	for _, required := range []string{"host=%2Fvar%2Flib%2Fpostgresql", "hostaddr=127.0.0.1", "port=5433", "sslmode=disable"} {
		if !strings.Contains(clean, required) {
			t.Fatalf("clean DSN lost %q: %q", required, clean)
		}
	}
}
