package dump

import (
	"reflect"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/db"
)

func TestTablesForSequenceCaptureIncludesOmittedParents(t *testing.T) {
	parent := db.Table{Schema: "public", Name: "events", RelKind: "p"}
	leaf := db.Table{Schema: "public", Name: "events_2024", RelKind: "r", PartitionOf: "public.events"}
	all := []db.Table{parent, leaf}
	exported := []db.Table{leaf}

	got := tablesForSequenceCapture(all, exported)
	want := []db.Table{parent, leaf}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tablesForSequenceCapture() = %+v, want %+v", got, want)
	}
}

func TestTablesForSequenceCaptureNoParentsUnchanged(t *testing.T) {
	users := db.Table{Schema: "public", Name: "users", RelKind: "r"}
	got := tablesForSequenceCapture([]db.Table{users}, []db.Table{users})
	if !reflect.DeepEqual(got, []db.Table{users}) {
		t.Fatalf("got %+v", got)
	}
}
