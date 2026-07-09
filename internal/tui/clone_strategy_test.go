package tui

import "testing"

func TestCloneStrategyDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"schema-replay", "DDL replay, then dump + restore (default; supports sanitization)"},
		{"template", "CREATE DATABASE … TEMPLATE on same server (fast, same instance only)"},
		{"logical-stream", "Table-by-table COPY streaming (best for large cross-server clones)"},
		{"physical-backup", "pg_basebackup cluster copy (entire data directory; needs target_dir)"},
		{"", "DDL replay, then dump + restore (default; supports sanitization)"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cloneStrategyDescription(tt.name); got != tt.want {
				t.Fatalf("cloneStrategyDescription(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestCloneStrategyChoicesMatchOptions(t *testing.T) {
	t.Parallel()
	if len(cloneStrategyChoices) != len(cloneStrategyOptions) {
		t.Fatalf("choices %d != options %d", len(cloneStrategyChoices), len(cloneStrategyOptions))
	}
	for i, name := range cloneStrategyChoices {
		if name != cloneStrategyOptions[i].Name {
			t.Fatalf("choice[%d] = %q, want %q", i, name, cloneStrategyOptions[i].Name)
		}
	}
}
