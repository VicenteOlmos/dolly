package clone

import "testing"

func TestDecomposeDSN(t *testing.T) {
	comps, err := DecomposeDSN("postgres://repl:secret@db-host:5433/mydb")
	if err != nil {
		t.Fatal(err)
	}
	if comps.Host != "db-host" || comps.Port != "5433" || comps.User != "repl" || comps.Password != "secret" || comps.Database != "mydb" {
		t.Fatalf("unexpected components: %+v", comps)
	}
}

func TestBuildPrimaryConninfo(t *testing.T) {
	got := BuildPrimaryConninfo(DSNComponents{
		Host:     "db-host",
		Port:     "5433",
		User:     "repl",
		Password: "sec'ret",
	})
	for _, sub := range []string{"host=db-host", "port=5433", "user=repl", "password='sec''ret'", "application_name=dolly_clone"} {
		if !contains(got, sub) {
			t.Fatalf("conninfo %q missing %q", got, sub)
		}
	}
}
