package tui

import (
	"strings"
	"testing"
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
