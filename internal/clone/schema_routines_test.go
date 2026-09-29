package clone

import "testing"

func TestOrderViewsPutsDependencyFirst(t *testing.T) {
	views := []viewRow{
		{schema: "public", name: "a_summary"},
		{schema: "public", name: "z_base"},
	}
	ordered, err := orderViews(views, map[string][]string{
		"public.a_summary": {"public.z_base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].name != "z_base" || ordered[1].name != "a_summary" {
		t.Fatalf("order = %s then %s", ordered[0].name, ordered[1].name)
	}
}

func TestOrderRoutinesRejectsCycle(t *testing.T) {
	rows := []routineRow{{oid: 1, name: "public.a"}, {oid: 2, name: "public.b"}}
	if _, err := orderRoutines(rows, []depEdge{{obj: 1, ref: 2}, {obj: 2, ref: 1}}); err == nil {
		t.Fatal("expected cyclic function dependency")
	}
}
