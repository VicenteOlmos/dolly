package connections

import (
	"strings"
	"testing"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want []string // substrings we want to see
		not  []string // substrings that must NOT appear
	}{
		{
			name: "password redacted",
			dsn:  "postgres://u:p@host/db",
			want: []string{"postgres://u:%2A%2A%2A@host/db"},
			not:  []string{":p@"},
		},
		{
			name: "no user no password",
			dsn:  "postgres://host/db",
			want: []string{"postgres://host/db"},
			not:  []string{"***"},
		},
		{
			name: "user without password",
			dsn:  "postgres://u@host/db",
			want: []string{"postgres://u@host/db"},
			not:  []string{"***", "%2A"},
		},
		{
			name: "URL-encoded password redacted",
			dsn:  "postgres://u:p%40ss@host/db",
			want: []string{"postgres://u:%2A%2A%2A@host/db"},
			not:  []string{"p%40ss"},
		},
		{
			name: "port and path preserved",
			dsn:  "postgres://u:p@h-a:5433/db_a?sslmode=prefer",
			want: []string{"u:%2A%2A%2A", "h-a:5433", "/db_a", "sslmode=prefer"},
			not:  []string{":p@"},
		},
		{
			name: "malformed DSN passthrough",
			dsn:  "not-a-valid-url|x",
			want: []string{"not-a-valid-url|x"},
			not:  nil,
		},
		{
			name: "password with quoted chars redacted",
			dsn:  "postgres://u:p%3Ax%3Fy@host/db",
			want: []string{"postgres://u:%2A%2A%2A@host/db"},
			not:  []string{"p%3Ax%3Fy"},
		},
		{
			name: "query password redacted",
			dsn:  "postgres://user@host/db?password=secret&sslmode=prefer",
			want: []string{"sslmode=prefer", "password=%2A%2A%2A"},
			not:  []string{"secret"},
		},
		{
			name: "query param pass key redacted",
			dsn:  "postgres://user@host/db?pass=mypass&connect_timeout=10",
			want: []string{"pass=%2A%2A%2A", "connect_timeout=10"},
			not:  []string{"mypass"},
		},
		{
			name: "query param pwd key redacted",
			dsn:  "postgres://user@host/db?pwd=abc123&host=other",
			want: []string{"pwd=%2A%2A%2A", "host=other"},
			not:  []string{"abc123"},
		},
		{
			name: "mixed-case secret query keys redacted",
			dsn:  "postgres://user@host/db?Password=SECRET&Pass=hidden&SSLMODE=require",
			want: []string{"Password=%2A%2A%2A", "Pass=%2A%2A%2A", "SSLMODE=require"},
			not:  []string{"SECRET", "hidden"},
		},
		{
			name: "multiple secret params redacted, non-secret preserved",
			dsn:  "postgres://user@host/db?password=s1&pass=s2&pwd=s3&sslmode=verify-full&connect_timeout=30",
			want: []string{"password=%2A%2A%2A", "pass=%2A%2A%2A", "pwd=%2A%2A%2A", "sslmode=verify-full", "connect_timeout=30"},
			not:  []string{"s1", "s2", "s3"},
		},
		{
			name: "both userinfo and query password redacted",
			dsn:  "postgres://u:p@host/db?password=secret&sslmode=require",
			want: []string{"u:%2A%2A%2A", "password=%2A%2A%2A", "sslmode=require"},
			not:  []string{":p@", "secret"},
		},
		{
			name: "sslpassword and passcode redacted",
			dsn:  "postgres://user@host/db?sslpassword=s1&passcode=p1&sslmode=require",
			want: []string{"sslpassword=%2A%2A%2A", "passcode=%2A%2A%2A", "sslmode=require"},
			not:  []string{"s1", "p1"},
		},
		{
			name: "substring match: auth_token redacted",
			dsn:  "postgres://user@host/db?auth_token=abc123&db=foo",
			want: []string{"auth_token=%2A%2A%2A", "db=foo"},
			not:  []string{"abc123"},
		},
		{
			name: "substring match: client_secret redacted",
			dsn:  "postgres://user@host/db?client_secret=x&app=myapp",
			want: []string{"client_secret=%2A%2A%2A", "app=myapp"},
			not:  []string{"x"},
		},
		{
			name: "substring match: sslcert and sslkey redacted",
			dsn:  "postgres://user@host/db?sslcert=mycert&sslkey=mykey&sslmode=require",
			want: []string{"sslcert=%2A%2A%2A", "sslkey=%2A%2A%2A", "sslmode=require"},
			not:  []string{"mycert", "mykey"},
		},
		{
			name: "channel_binding preserved",
			dsn:  "postgres://user@host/db?channel_binding=xxx&db=foo",
			want: []string{"channel_binding=xxx", "db=foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactDSN(tt.dsn)
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Errorf("RedactDSN(%q) missing %q in %q", tt.dsn, part, got)
				}
			}
			for _, part := range tt.not {
				if strings.Contains(got, part) {
					t.Errorf("RedactDSN(%q) contains forbidden %q in %q", tt.dsn, part, got)
				}
			}
		})
	}
}

func TestRedactDSNMalformedPostgresURLFailsClosed(t *testing.T) {
	const secret = "secret%zz"
	got := RedactDSN("postgres://user:" + secret + "@localhost/db")
	if strings.Contains(got, secret) {
		t.Fatalf("RedactDSN leaked malformed URL credential: %q", got)
	}
}

func TestRedactDSNRedactsMalformedSecretQueryValue(t *testing.T) {
	const secret = "secret%zz"
	got := RedactDSN("postgres://user@localhost/db?sslmode=require&password=" + secret)
	if strings.Contains(got, secret) {
		t.Fatalf("RedactDSN leaked malformed query credential: %q", got)
	}
	if !strings.Contains(got, "sslmode=require") {
		t.Fatalf("RedactDSN lost safe query content: %q", got)
	}
}

func TestRedactMessage(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
		not  []string
	}{
		{
			name: "clean URL redacted",
			in:   "postgres://u:p@host/db?password=s&sslmode=prefer",
			want: []string{"u:%2A%2A%2A", "password=%2A%2A%2A"},
			not:  []string{":p@", "password=s"},
		},
		{
			name: "embedded DSN in error text",
			in:   "failed to connect to postgres://u:p@host/db?password=secret&sslmode=require: timeout",
			want: []string{"u:%2A%2A%2A", "password=***"},
			not:  []string{":p@", "password=secret"},
		},
		{
			name: "libpq keyword DSN redacted",
			in:   "host=db.example.com user=app password=secret dbname=app",
			want: []string{"password=***", "host=db.example.com"},
			not:  []string{"password=secret"},
		},
		{
			name: "quoted libpq password with whitespace redacted",
			in:   "connection failed: host=db password='secret tail' dbname=app",
			want: []string{"password=***", "dbname=app"},
			not:  []string{"secret", "tail"},
		},
		{
			name: "auth_token in text redacted",
			in:   "connection refused: auth_token=my-token-123 db=prod",
			want: []string{"auth_token=***", "db=prod"},
			not:  []string{"my-token"},
		},
		{
			name: "no secrets unchanged",
			in:   "normal error message",
			want: []string{"normal error message"},
			not:  nil,
		},
		{
			name: "sslpassword in error",
			in:   "pg_hba entry sslpassword=abc123 no encryption",
			want: []string{"sslpassword=***"},
			not:  []string{"sslpassword=abc123"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactMessage(tt.in)
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Errorf("RedactMessage(%q) missing %q in %q", tt.in, part, got)
				}
			}
			for _, part := range tt.not {
				if strings.Contains(got, part) {
					t.Errorf("RedactMessage(%q) contains forbidden %q in %q", tt.in, part, got)
				}
			}
		})
	}
}
