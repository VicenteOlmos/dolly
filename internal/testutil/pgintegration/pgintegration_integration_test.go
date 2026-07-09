//go:build integration

package pgintegration

import (
	"testing"
)

func TestOpenSkipsWhenDSNUnset(t *testing.T) {
	t.Setenv(EnvDSN, "")
	Open(t)
	t.Fatal("Open should have skipped")
}

func TestOpenSkipsWhenUnreachable(t *testing.T) {
	t.Setenv(EnvDSN, "postgres://u:p@127.0.0.1:1/db_x?sslmode=disable&connect_timeout=1")
	Open(t)
	t.Fatal("Open should have skipped")
}
