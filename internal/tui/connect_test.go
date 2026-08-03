package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestConnectionDraftDSN(t *testing.T) {
	tests := []struct {
		name string
		in   ConnectionDraft
		want []string
	}{
		{
			name: "default port and sslmode",
			in: ConnectionDraft{
				Host:     "h-a",
				Database: "db_a",
				User:     "u",
				Password: "p",
			},
			want: []string{
				"postgres://",
				"u:p@",
				"h-a:5432",
				"/db_a",
				"sslmode=verify-full",
				"channel_binding=require",
			},
		},
		{
			name: "explicit port",
			in: ConnectionDraft{
				Host:     "h-b",
				Port:     "5433",
				Database: "db_b",
				User:     "u",
				Password: "p",
			},
			want: []string{
				"h-b:5433",
				"u:p@",
				"/db_b",
			},
		},
		{
			name: "password url encoding",
			in: ConnectionDraft{
				Host:     "h-a",
				Database: "db_a",
				User:     "u@h",
				Password: "p@x:y?z",
			},
			want: []string{
				"u%40h",
				"p%40x%3Ay%3Fz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.DSN()
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Fatalf("DSN() = %q, missing %q", got, part)
				}
			}
		})
	}
}

func TestDBConnOptionsPrepareDSN(t *testing.T) {
	tests := []struct {
		name    string
		opts    dbConnOptions
		dsn     string
		want    []string
		lacks   []string
		wantErr bool
	}{
		{
			name: "enabled timeout injects param",
			opts: dbConnOptions{statementTimeout: "5min"},
			dsn:  "postgres://u:p@host/db?sslmode=disable",
			want: []string{"statement_timeout=5min"},
		},
		{
			name:  "disabled timeout unchanged",
			opts:  dbConnOptions{statementTimeout: "0"},
			dsn:   "postgres://u:p@host/db?sslmode=disable",
			want:  []string{"sslmode=disable"},
			lacks: []string{"statement_timeout"},
		},
		{
			name:    "malformed dsn fails closed",
			opts:    dbConnOptions{statementTimeout: "5min"},
			dsn:     "not-a-valid-dsn",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.opts.prepareDSN(tt.dsn)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Fatalf("prepareDSN() = %q, missing %q", got, part)
				}
			}
			for _, part := range tt.lacks {
				if strings.Contains(got, part) {
					t.Fatalf("prepareDSN() = %q, contains forbidden %q", got, part)
				}
			}
		})
	}
}

func TestDBConnOptionsEffectiveMaxOpenConns(t *testing.T) {
	tests := []struct {
		name string
		opts dbConnOptions
		want int
	}{
		{name: "default", opts: dbConnOptions{}, want: 5},
		{name: "zero", opts: dbConnOptions{maxOpenConns: 0}, want: 5},
		{name: "negative", opts: dbConnOptions{maxOpenConns: -1}, want: 5},
		{name: "custom", opts: dbConnOptions{maxOpenConns: 12}, want: 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.effectiveMaxOpenConns(); got != tt.want {
				t.Fatalf("effectiveMaxOpenConns() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPostgresSchemaLoaderMalformedDSNRedactsSecrets(t *testing.T) {
	secret := "s3cret-p@ss"
	dsn := "postgres://user:" + secret + "@localhost/db"
	loader := postgresSchemaLoader{dbConnOptions: dbConnOptions{statementTimeout: "5min"}}
	_, err := loader.openAndPing(context.Background(), "not-valid|"+dsn)
	if err == nil {
		t.Fatal("expected error for malformed DSN")
	}
	msg := err.Error()
	if strings.Contains(msg, secret) {
		t.Fatalf("error leaked password: %q", msg)
	}
	if !strings.Contains(msg, "configure connection") {
		t.Fatalf("error = %q, want configure connection prefix", msg)
	}
}

func TestPostgresSchemaLoaderPrepareDSNUsesSetDSNParam(t *testing.T) {
	loader := postgresSchemaLoader{dbConnOptions: dbConnOptions{statementTimeout: "2min"}}
	got, err := loader.prepareDSN("host=localhost port=5432 dbname=mydb")
	if err != nil {
		t.Fatal(err)
	}
	want, err := connections.SetDSNParam("host=localhost port=5432 dbname=mydb", "statement_timeout", "2min")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("prepareDSN() = %q, want %q", got, want)
	}
}

func TestEnsureConnectTimeout(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
		not  []string
	}{
		{
			name: "adds connect_timeout when missing",
			dsn:  "postgres://u:p@host/db?sslmode=prefer",
			want: "connect_timeout=10",
		},
		{
			name: "preserves existing connect_timeout",
			dsn:  "postgres://u:p@host/db?connect_timeout=5",
			want: "connect_timeout=5",
			not:  []string{"connect_timeout=10"},
		},
		{
			name: "preserves other params",
			dsn:  "postgres://u:p@host/db?sslmode=require",
			want: "sslmode=require",
		},
		{
			name: "non-pg DSN unchanged",
			dsn:  "not-a-url|x",
			want: "not-a-url|x",
		},
		{
			name: "keyword form adds connect_timeout",
			dsn:  "host=localhost port=5432 dbname=mydb",
			want: "connect_timeout=10",
		},
		{
			name: "keyword form preserves existing connect_timeout",
			dsn:  "host=localhost port=5432 connect_timeout=5",
			want: "connect_timeout=5",
			not:  []string{"connect_timeout=10"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureConnectTimeout(tt.dsn)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("ensureConnectTimeout(%q) = %q, missing %q", tt.dsn, got, tt.want)
			}
			for _, n := range tt.not {
				if strings.Contains(got, n) {
					t.Fatalf("ensureConnectTimeout(%q) = %q, contains forbidden %q", tt.dsn, got, n)
				}
			}
		})
	}
}
