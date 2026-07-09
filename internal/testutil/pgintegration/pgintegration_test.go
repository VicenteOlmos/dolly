package pgintegration

import (
	"context"
	"testing"
	"time"
)

func TestEnvDSNName(t *testing.T) {
	if EnvDSN != "DOLLY_TEST_PG_DSN" {
		t.Fatalf("EnvDSN = %q", EnvDSN)
	}
}

func TestPingDSNUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dsn := "postgres://u:p@127.0.0.1:1/db_x?sslmode=disable&connect_timeout=1"
	_, err := pingDSN(ctx, dsn)
	if err == nil {
		t.Fatal("expected ping error for unreachable host")
	}
}

func TestPingDSNEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := pingDSN(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty DSN")
	}
}
