//go:build integration

package main

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/testutil/pgintegration"
)

func integrationBaseDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgintegration.EnvDSN)
	if dsn == "" {
		t.Skipf("set %s to run integration tests", pgintegration.EnvDSN)
	}
	return dsn
}

func integrationURLKeywordDSNs(t *testing.T, base string) []struct{ name, dsn string } {
	t.Helper()
	kw := keywordDSNFromURL(t, base)
	return []struct{ name, dsn string }{{"url", base}, {"keyword", kw}}
}

func keywordDSNFromURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse base DSN: %v", err)
	}
	pass, _ := u.User.Password()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	return "host=" + u.Hostname() + " port=" + port + " dbname=" + strings.TrimPrefix(u.Path, "/") +
		" user=" + u.User.Username() + " password=" + pass + " sslmode=disable"
}

func openPingCheckTimeout(t *testing.T, dsn, want string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	var timeout string
	if err := db.QueryRowContext(ctx2, "SHOW statement_timeout").Scan(&timeout); err != nil {
		_ = db.Close()
		t.Fatalf("SHOW statement_timeout: %v", err)
	}
	if timeout != want {
		_ = db.Close()
		t.Fatalf("statement_timeout = %q, want %s", timeout, want)
	}
	return db
}

func TestStatementTimeoutInjectionIntegration(t *testing.T) {
	base := integrationBaseDSN(t)
	for _, tc := range integrationURLKeywordDSNs(t, base) {
		t.Run(tc.name, func(t *testing.T) {
			injected, err := appendQueryParam(tc.dsn, "statement_timeout", "2s")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "keyword" && strings.Contains(injected, "://") {
				t.Fatalf("keyword DSN re-encoded as URL: %q", injected)
			}
			db := openPingCheckTimeout(t, injected, "2s")
			t.Cleanup(func() { _ = db.Close() })
		})
	}
}

func TestStatementTimeoutShortSucceedsLongAbortsIntegration(t *testing.T) {
	base := integrationBaseDSN(t)
	for _, tc := range integrationURLKeywordDSNs(t, base) {
		t.Run(tc.name, func(t *testing.T) {
			shortDSN, err := appendQueryParam(tc.dsn, "statement_timeout", "10min")
			if err != nil {
				t.Fatal(err)
			}
			db := openPingCheckTimeout(t, shortDSN, "10min")
			t.Cleanup(func() { _ = db.Close() })

			longDSN, err := appendQueryParam(tc.dsn, "statement_timeout", "1ms")
			if err != nil {
				t.Fatal(err)
			}
			dbSlow, err := sql.Open("pgx", longDSN)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			t.Cleanup(func() { _ = dbSlow.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var n int
			err = dbSlow.QueryRowContext(ctx, "SELECT pg_sleep(5)").Scan(&n)
			if err == nil {
				t.Fatal("expected pg_sleep to fail with 1ms statement_timeout")
			}
		})
	}
}

func TestStatementTimeoutSubprocessDSNStripIntegration(t *testing.T) {
	base := integrationBaseDSN(t)
	withDash, err := appendQueryParam(base, "statement_timeout", "30s")
	if err != nil {
		t.Fatal(err)
	}
	withKW, err := connections.SetDSNParam(keywordDSNFromURL(t, base)+" options='--client-min-messages=notice'", "statement_timeout", "30s")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, dsn string }{{"url", withDash}, {"keyword", withKW}} {
		t.Run(tc.name, func(t *testing.T) {
			clean, _, err := connections.SubprocessDSN(tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(clean, "statement_timeout") {
				t.Fatalf("SubprocessDSN leaked statement_timeout: %q", clean)
			}
			for _, arg := range strings.Fields(clean) {
				if strings.HasPrefix(arg, "--") {
					t.Fatalf("argv-like token in clean DSN: %q", arg)
				}
			}
		})
	}
}
