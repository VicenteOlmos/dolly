package dump

import (
	"errors"
	"fmt"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNoTablesErrorIsSentinel(t *testing.T) {
	err := &NoTablesError{Schemas: []string{"app"}}
	if !errors.Is(err, ErrNoTables) {
		t.Fatal("expected ErrNoTables sentinel")
	}
	if !IsNoTablesError(err) {
		t.Fatal("expected IsNoTablesError")
	}
	if got := err.Error(); got != `no tables found in schema(s) app` {
		t.Fatalf("error = %q", got)
	}
}

func TestNoTablesErrorDefaultPublicLabel(t *testing.T) {
	err := &NoTablesError{}
	if got := err.Error(); got != `no tables found in schema(s) public` {
		t.Fatalf("error = %q", got)
	}
}

func TestGuardSelectedTablesPassesWithTables(t *testing.T) {
	tables := []db.Table{{Schema: "public", Name: "users"}}
	if err := guardSelectedTables(tables, []string{"public"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGuardSelectedTablesRejectsEmpty(t *testing.T) {
	err := guardSelectedTables(nil, []string{"app", "public"})
	if !IsNoTablesError(err) {
		t.Fatalf("error = %v", err)
	}
	var noTables *NoTablesError
	if !errors.As(err, &noTables) {
		t.Fatal("expected NoTablesError type")
	}
	if len(noTables.Schemas) != 2 || noTables.Schemas[0] != "app" {
		t.Fatalf("schemas = %v", noTables.Schemas)
	}
}

func TestSlowRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "recovery conflict", err: &pgconn.PgError{Code: "40001"}, want: true},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: true},
		{name: "connection failure", err: &pgconn.PgError{Code: "08006"}, want: true},
		{name: "wrapped", err: fmt.Errorf("iterate: %w", &pgconn.PgError{Code: "40001"}), want: true},
		{name: "data error", err: &pgconn.PgError{Code: "22012"}},
		{name: "plain error", err: errors.New("connection reset")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slowRetryable(tt.err); got != tt.want {
				t.Fatalf("slowRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}
