package connections

import (
	"strings"
	"testing"
)

func TestConnectionDSN(t *testing.T) {
	tests := []struct {
		name string
		in   Connection
		want []string
	}{
		{
			name: "default port and sslmode verify-full",
			in: Connection{
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
			name: "explicit sslmode require",
			in: Connection{
				Host:     "h-a",
				Database: "db_a",
				User:     "u",
				Password: "p",
				SSLMODE:  "require",
			},
			want: []string{
				"sslmode=require",
			},
		},
		{
			name: "explicit sslmode disable",
			in: Connection{
				Host:     "h-a",
				Database: "db_a",
				User:     "u",
				Password: "p",
				SSLMODE:  "disable",
			},
			want: []string{
				"sslmode=disable",
			},
		},
		{
			name: "explicit sslmode verify-full",
			in: Connection{
				Host:     "h-a",
				Database: "db_a",
				User:     "u",
				Password: "p",
				SSLMODE:  "verify-full",
			},
			want: []string{
				"sslmode=verify-full",
			},
		},
		{
			name: "empty sslmode defaults to verify-full",
			in: Connection{
				Host:     "h-a",
				Database: "db_a",
				User:     "u",
				Password: "p",
				SSLMODE:  "",
			},
			want: []string{
				"sslmode=verify-full",
				"channel_binding=require",
			},
		},
		{
			name: "password url encoding preserved",
			in: Connection{
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
		{
			name: "explicit port used",
			in: Connection{
				Host:     "h-a",
				Port:     "5433",
				Database: "db_a",
				User:     "u",
				Password: "p",
			},
			want: []string{
				"h-a:5433",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.DSN()
			for _, part := range tt.want {
				if !strings.Contains(got, part) {
					t.Errorf("DSN() = %q, missing %q", got, part)
				}
			}
		})
	}
}

func TestConnectionSignatureNormalizesPort(t *testing.T) {
	withPort := Connection{Host: "h", Port: "5433", Database: "d", User: "u"}
	emptyPort := Connection{Host: "h", Database: "d", User: "u"}
	if withPort.Signature() == emptyPort.Signature() {
		t.Fatal("expected different signatures for explicit vs default port")
	}
	defaultPort := Connection{Host: "h", Port: "5432", Database: "d", User: "u"}
	if defaultPort.Signature() != emptyPort.Signature() {
		t.Fatalf("default port signatures differ: %q vs %q", defaultPort.Signature(), emptyPort.Signature())
	}
}
