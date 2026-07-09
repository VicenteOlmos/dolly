//go:build integration

package pgintegration

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestOpenSkipsWhenDSNUnset(t *testing.T) {
	t.Setenv(EnvDSN, "")
	Open(t)
	t.Fatal("Open should have skipped")
}

func TestSetupMainDBFailsWhenConfiguredDSNIsUnreachable(t *testing.T) {
	t.Setenv(EnvDSN, "postgres://u:p@127.0.0.1:1/db_x?sslmode=disable&connect_timeout=1")
	db, err := SetupMainDB()
	if err == nil {
		if db != nil {
			_ = db.Close()
		}
		t.Fatal("expected unreachable DSN error")
	}
}

func TestOpenFailsWhenConfiguredDSNIsUnreachable(t *testing.T) {
	if os.Getenv("DOLLY_TEST_OPEN_UNREACHABLE_CHILD") == "1" {
		t.Setenv(EnvDSN, "postgres://u:p@127.0.0.1:1/db_x?sslmode=disable&connect_timeout=1")
		Open(t)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestOpenFailsWhenConfiguredDSNIsUnreachable$")
	cmd.Env = append(os.Environ(), "DOLLY_TEST_OPEN_UNREACHABLE_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("child test should fail, output:\n%s", out)
	}
	if !strings.Contains(string(out), "database unreachable") {
		t.Fatalf("child output missing unreachable error:\n%s", out)
	}
}
