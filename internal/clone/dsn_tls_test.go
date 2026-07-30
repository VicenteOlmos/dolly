package clone

import (
	"net/url"
	"testing"
)

func TestTLSParams(t *testing.T) {
	rootCert := "/tmp/ca.crt"
	sourceDSN := "postgres://repl:secret@db-host:5433/mydb?sslmode=verify-full&sslrootcert=" + url.QueryEscape(rootCert) + "&channel_binding=require"
	got, err := TLSParams(sourceDSN)
	if err != nil {
		t.Fatal(err)
	}
	if got["PGSSLMODE"] != "verify-full" {
		t.Fatalf("PGSSLMODE = %q", got["PGSSLMODE"])
	}
	if got["PGSSLROOTCERT"] != rootCert {
		t.Fatalf("PGSSLROOTCERT = %q", got["PGSSLROOTCERT"])
	}
	if got["PGCHANNELBINDING"] != "require" {
		t.Fatalf("PGCHANNELBINDING = %q", got["PGCHANNELBINDING"])
	}
}

func TestTLSParamsOmitsUnset(t *testing.T) {
	got, err := TLSParams("postgres://repl@db-host:5433/mydb?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if got["PGSSLMODE"] != "disable" {
		t.Fatalf("PGSSLMODE = %q", got["PGSSLMODE"])
	}
	if _, ok := got["PGSSLROOTCERT"]; ok {
		t.Fatalf("unexpected PGSSLROOTCERT: %v", got)
	}
	if _, ok := got["PGCHANNELBINDING"]; ok {
		t.Fatalf("unexpected PGCHANNELBINDING: %v", got)
	}
}
