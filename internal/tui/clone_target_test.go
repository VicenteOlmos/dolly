package tui

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

func TestResolveCloneDraftTargetDSNCurrentRefreshesStaleDSN(t *testing.T) {
	t.Parallel()
	current := "postgres://u:secret@fresh-host/db_stub"
	draft := &CloneDraft{
		TargetSource: TargetSourceCurrent,
		TargetDSN:    "postgres://stale@old-host/wrong",
	}
	resolveCloneDraftTargetDSN(draft, func() string { return current }, nil)
	if draft.TargetDSN != current {
		t.Fatalf("TargetDSN = %q, want %q", draft.TargetDSN, current)
	}
}

func TestResolveCloneDraftTargetDSNSavedRefreshesFromStore(t *testing.T) {
	t.Parallel()
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	want := profileDSN(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	draft := &CloneDraft{
		TargetSource:      TargetSourceSaved,
		TargetProfileName: "staging",
		TargetDSN:         "postgres://stale@old-host/wrong",
	}
	resolveCloneDraftTargetDSN(draft, nil, store)
	if draft.TargetDSN != want {
		t.Fatalf("TargetDSN = %q, want %q", draft.TargetDSN, want)
	}
}

func TestResolveCloneDraftTargetDSNSavedPicksFirstProfile(t *testing.T) {
	t.Parallel()
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	draft := &CloneDraft{TargetSource: TargetSourceSaved, TargetDSN: "postgres://stale@old-host/wrong"}
	resolveCloneDraftTargetDSN(draft, nil, store)
	if draft.TargetProfileName != "staging" {
		t.Fatalf("TargetProfileName = %q, want staging", draft.TargetProfileName)
	}
	if !strings.Contains(draft.TargetDSN, "db.example.com") {
		t.Fatalf("TargetDSN = %q, want staging host", draft.TargetDSN)
	}
}

func TestResolveCloneDraftTargetDSNManualKeepsTypedDSN(t *testing.T) {
	t.Parallel()
	manual := "postgres://u:secret@manual-host/mydb"
	draft := &CloneDraft{
		TargetSource: TargetSourceManual,
		TargetDSN:    manual,
	}
	resolveCloneDraftTargetDSN(draft, func() string { return "postgres://u:other@fresh/db" }, nil)
	if draft.TargetDSN != manual {
		t.Fatalf("TargetDSN = %q, want manual %q", draft.TargetDSN, manual)
	}
}

func TestCycleStrategyRefreshesTargetDSNFromCurrentSource(t *testing.T) {
	t.Parallel()
	current := "postgres://u:secret@fresh-host/db_stub"
	draft := &CloneDraft{
		TargetSource: TargetSourceCurrent,
		TargetDSN:    "postgres://stale@old-host/wrong",
		Strategy:     "schema-replay",
	}
	cs := &cloneScreen{draft: draft, getConnDSN: func() string { return current }}
	cs.cycleStrategy(1)
	if draft.Strategy != "template" {
		t.Fatalf("Strategy = %q, want template", draft.Strategy)
	}
	if draft.TargetDSN != current {
		t.Fatalf("TargetDSN = %q, want refreshed %q", draft.TargetDSN, current)
	}
}

func TestCycleStrategyRefreshesTargetDSNFromSavedProfile(t *testing.T) {
	t.Parallel()
	store := newMockConnectionStore(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	want := profileDSN(connections.Connection{
		Name: "staging", Host: "db.example.com", Port: "5432", Database: "app", User: "app", Password: "x",
	})
	draft := &CloneDraft{
		TargetSource:      TargetSourceSaved,
		TargetProfileName: "staging",
		TargetDSN:         "postgres://stale@old-host/wrong",
		Strategy:          "schema-replay",
	}
	cs := &cloneScreen{draft: draft, store: store}
	cs.cycleStrategy(1)
	if draft.TargetDSN != want {
		t.Fatalf("TargetDSN = %q, want %q", draft.TargetDSN, want)
	}
}

func TestCycleStrategyKeepsManualTargetDSN(t *testing.T) {
	t.Parallel()
	manual := "postgres://u:secret@manual-host/mydb"
	draft := &CloneDraft{
		TargetSource: TargetSourceManual,
		TargetDSN:    manual,
		Strategy:     "schema-replay",
	}
	cs := &cloneScreen{
		draft:      draft,
		getConnDSN: func() string { return "postgres://u:other@fresh/db" },
	}
	cs.cycleStrategy(1)
	if draft.TargetDSN != manual {
		t.Fatalf("TargetDSN = %q, want manual %q", draft.TargetDSN, manual)
	}
}

func TestCloneNeedsUnsanitizedWarning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		strategy string
		enabled  bool
		want     bool
	}{
		{"schema-replay sanitized", "schema-replay", true, false},
		{"template with sanitization", "template", true, true},
		{"logical-stream with sanitization", "logical-stream", true, true},
		{"physical-backup with sanitization", "physical-backup", true, true},
		{"disabled sanitization", "schema-replay", false, true},
		{"empty strategy sanitized", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cloneNeedsUnsanitizedWarning(tt.strategy, tt.enabled); got != tt.want {
				t.Fatalf("cloneNeedsUnsanitizedWarning(%q, %v) = %v, want %v", tt.strategy, tt.enabled, got, tt.want)
			}
		})
	}
}
