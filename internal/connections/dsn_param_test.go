package connections

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSetDSNParam(t *testing.T) {
	const k, v = "statement_timeout", "5min"
	const v30, v1m = "30s", "1min"
	assertMalformed := func(t *testing.T, dsn string, err error, secret string) {
		t.Helper()
		if err == nil {
			t.Fatalf("want ErrMalformedDSN for %q", dsn)
		}
		if !errors.Is(err, ErrMalformedDSN) {
			t.Fatalf("err %v want ErrMalformedDSN", err)
		}
		if dsn != "" && strings.Contains(err.Error(), dsn) {
			t.Fatalf("error leaks DSN %q", dsn)
		}
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaks secret %q", secret)
		}
	}
	type tc struct {
		name, dsn, key, val string
		want                string
		has, lacks          []string
		noURL               bool
		countKey            string
		idem, passthrough   bool
		err                 bool
		secret              string
		after               [2]string
	}
	cases := []tc{
		{name: "T1/host", dsn: "host", key: k, val: v, err: true},
		{name: "T1/empty", dsn: "", key: k, val: v, err: true},
		{name: "T1/space", dsn: "   ", key: k, val: v, err: true},
		{name: "T1/empty-key", dsn: "=value", key: k, val: v, err: true},
		{name: "T1/no-eq", dsn: "foo bar", key: k, val: v, err: true},
		{name: "T1/raw-host", dsn: "localhost", key: k, val: v30, err: true},
		{name: "T1/garbage", dsn: "not a dsn at all", key: k, val: v30, err: true},
		{name: "T1/empty-param-key", dsn: "host=localhost", key: "", val: v, err: true},
		{name: "T1/bad-url", dsn: "postgres://[::1%lo]:5432/db", key: k, val: v30, err: true},
		{name: "T1/https", dsn: "https://user@localhost/db", key: k, val: v30, err: true},
		{name: "T1/trail", dsn: "host=localhost garbage", key: k, val: v30, err: true},
		{name: "T1/unterm", dsn: "host=localhost options='--client", key: k, val: v30, err: true},
		{name: "T1/empty-later", dsn: "host=localhost =foo", key: k, val: v30, err: true},
		{name: "T2/keyword-form", dsn: "host=localhost port=5432 dbname=mydb user=admin password='p@ss word'", key: k, val: v,
			noURL: true, has: []string{"host=localhost", "port=5432", "dbname=mydb", "user=admin", "statement_timeout=5min"}},
		{name: "T4/dash", dsn: "host=localhost options='--client flag'", key: k, val: v30,
			has: []string{"options='--client flag'", "statement_timeout=30s"}},
		{name: "T5/dedup", dsn: "host=localhost statement_timeout=1s port=5432", key: k, val: v,
			want: "host=localhost statement_timeout=5min port=5432", countKey: k, idem: true},
		{name: "T7/quoted-eq", dsn: "host=localhost options='-c work_mem=64MB' port=5432", key: k, val: "30s",
			has: []string{"options='-c work_mem=64MB'", "host=localhost", "port=5432", "statement_timeout=30s"}},
		{name: "T7/doubled-quote", dsn: "host=localhost name=O''Brien port=5432", key: k, val: "30s",
			has: []string{"name=O''Brien", "host=localhost", "port=5432", "statement_timeout=30s"}},
		{name: "T7/backslash", dsn: `host=localhost data=a\\b port=5432`, key: k, val: "30s",
			has: []string{`data=a\\b`, "host=localhost", "port=5432", "statement_timeout=30s"}, lacks: []string{`data=a\b`}},
		{name: "T7/search-path", dsn: "host=localhost options='-c search_path=public,pg_catalog'", key: k, val: v30,
			has: []string{"options='-c search_path=public,pg_catalog'", "statement_timeout=30s"}},
		{name: "T7/spaces", dsn: "host=localhost application_name='my app v2'", key: k, val: v1m,
			has: []string{"application_name='my app v2'", "statement_timeout=1min"}},
		{name: "T7/append-after-quote", dsn: "host=localhost options='-c work_mem=64MB'", key: k, val: v,
			has: []string{"options='-c work_mem=64MB'", "statement_timeout=5min"}, after: [2]string{"options=", "statement_timeout="}},
		{name: "disabled/url-empty", dsn: "postgres://localhost/db", key: k, val: "", passthrough: true},
		{name: "disabled/url-zero", dsn: "postgres://localhost/db", key: k, val: "0", passthrough: true},
		{name: "disabled/kw-empty", dsn: "host=localhost", key: k, val: "", passthrough: true},
		{name: "disabled/kw-zero", dsn: "host=localhost", key: k, val: "0", passthrough: true},
		{name: "url/no-query", dsn: "postgres://user@localhost/db", key: k, val: v30,
			want: "postgres://user@localhost/db?statement_timeout=30s"},
		{name: "url/existing-query", dsn: "postgres://user@localhost/db?sslmode=disable", key: k, val: v30,
			want: "postgres://user@localhost/db?sslmode=disable&statement_timeout=30s"},
		{name: "url/replace", dsn: "postgres://user@localhost/db?statement_timeout=1s&sslmode=disable", key: k, val: "5min",
			want: "postgres://user@localhost/db?sslmode=disable&statement_timeout=5min"},
		{name: "url/userinfo", dsn: "postgres://alice:secret@localhost/db", key: k, val: v30,
			want: "postgres://alice:secret@localhost/db?statement_timeout=30s"},
		{name: "url/encoded-userinfo", dsn: "postgres://user%40domain:p%40ss@localhost/db", key: k, val: v30,
			want: "postgres://user%40domain:p%40ss@localhost/db?statement_timeout=30s"},
		{name: "url/preserve-params", dsn: "postgres://localhost/db?sslmode=require&application_name=dolly", key: k, val: v1m,
			want: "postgres://localhost/db?application_name=dolly&sslmode=require&statement_timeout=1min"},
		{name: "url/postgresql-scheme", dsn: "postgresql://localhost/db", key: k, val: v30,
			want: "postgresql://localhost/db?statement_timeout=30s"},
		{name: "url/idempotent", dsn: "postgres://localhost/db?sslmode=disable", key: k, val: v30, idem: true},
		{name: "kw/single", dsn: "host=localhost", key: k, val: v, want: "host=localhost statement_timeout=5min"},
		{name: "kw/multi", dsn: "host=localhost port=5432 dbname=mydb", key: k, val: v,
			want: "host=localhost port=5432 dbname=mydb statement_timeout=5min"},
		{name: "kw/replace-same-case", dsn: "host=localhost statement_timeout=1s port=5432", key: k, val: v,
			want: "host=localhost statement_timeout=5min port=5432"},
		{name: "kw/replace-upper", dsn: "host=localhost STATEMENT_TIMEOUT=1s port=5432", key: k, val: v,
			want: "host=localhost STATEMENT_TIMEOUT=5min port=5432"},
		{name: "kw/replace-mixed", dsn: "host=localhost Statement_Timeout=1s port=5432", key: k, val: v,
			want: "host=localhost Statement_Timeout=5min port=5432"},
		{name: "kw/replace-mixed2", dsn: "host=localhost STATEMENT_timeout=1s port=5432", key: k, val: v,
			want: "host=localhost STATEMENT_timeout=5min port=5432"},
		{name: "kw/idempotent", dsn: "host=localhost statement_timeout=5min port=5432", key: k, val: v30, idem: true},
		{name: "T3/kw-secret", dsn: "garbageSuperSecret", key: k, val: v, err: true, secret: "SuperSecret"},
		{name: "T3/quoted-secret", dsn: "garbageabc123secret", key: k, val: v, err: true, secret: "abc123secret"},
		{name: "golden/socket", dsn: "host=/var/run/postgresql", key: k, val: "10s",
			has: []string{"host=/var/run/postgresql", "statement_timeout=10s"}},
		{name: "golden/full", dsn: "host=localhost port=5432 dbname=postgres user=postgres password=secret sslmode=disable", key: k, val: v30,
			has: []string{"host=localhost", "port=5432", "dbname=postgres", "user=postgres", "password=secret", "sslmode=disable", "statement_timeout=30s"}},
		{name: "golden/quoted-pw", dsn: "host=localhost port=5432 dbname=source_db user=admin password='s3cret'", key: k, val: v,
			has: []string{"password='s3cret'", "statement_timeout=5min"}},
		{name: "golden/dbname-only", dsn: "dbname=postgres", key: k, val: "2min",
			has: []string{"dbname=postgres", "statement_timeout=2min"}},
		{name: "edge/multi-space", dsn: "host=localhost  port=5432   dbname=mydb", key: k, val: v,
			has: []string{"host=localhost", "port=5432", "statement_timeout=5min"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SetDSNParam(tt.dsn, tt.key, tt.val)
			if tt.err {
				assertMalformed(t, tt.dsn, err, tt.secret)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.passthrough && got != tt.dsn {
				t.Fatalf("passthrough: got %q want %q", got, tt.dsn)
			}
			if tt.want != "" && got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
			for _, s := range tt.has {
				if !strings.Contains(got, s) {
					t.Fatalf("missing %q in %q", s, got)
				}
			}
			for _, s := range tt.lacks {
				if strings.Contains(got, s) {
					t.Fatalf("unexpected %q in %q", s, got)
				}
			}
			if tt.noURL && (strings.HasPrefix(got, "postgres") || strings.Contains(got, "://") || strings.Contains(got, "%")) {
				t.Fatalf("re-encoded as URL: %q", got)
			}
			if tt.countKey != "" && strings.Count(got, tt.countKey) != 1 {
				t.Fatalf("%q count=%d want 1 in %q", tt.countKey, strings.Count(got, tt.countKey), got)
			}
			if tt.after[0] != "" && strings.Index(got, tt.after[1]) < strings.Index(got, tt.after[0]) {
				t.Fatalf("%q not after %q in %q", tt.after[1], tt.after[0], got)
			}
			if tt.idem {
				got2, err := SetDSNParam(got, tt.key, tt.val)
				if err != nil {
					t.Fatal(err)
				}
				if got != got2 {
					t.Fatalf("not idempotent:\n  %q\n  %q", got, got2)
				}
			}
		})
	}
	for _, dsn := range []string{"postgres://localhost/db", "host=localhost port=5432",
		"host=localhost password=SuperSecret port=5432", "host=localhost token='abc123secret'"} {
		if _, err := SetDSNParam(dsn, k, "1s"); err != nil {
			t.Fatalf("T3/T6 %q: %v", dsn, err)
		}
	}
}

func TestSetDSNParam_pgconnParity(t *testing.T) {
	const k, v = "statement_timeout", "30s"
	for _, dsn := range []string{"host=localhost port=5432 dbname=postgres user=postgres",
		"host=localhost options='-c work_mem=64MB'", "postgres://localhost/db?sslmode=disable",
		"postgresql://user:pass@localhost:5432/mydb"} {
		if _, err := pgconn.ParseConfig(dsn); err != nil {
			t.Fatalf("pgconn reject %q: %v", dsn, err)
		}
		got, err := SetDSNParam(dsn, k, v)
		if err != nil {
			t.Fatalf("set %q: %v", dsn, err)
		}
		if _, err := pgconn.ParseConfig(got); err != nil {
			t.Fatalf("pgconn reject output %q -> %q: %v", dsn, got, err)
		}
	}
}
