package tui

import (
	"strings"
	"testing"
)

func TestRenderCapabilitiesStripWidths(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		contain []string
	}{
		{
			name:    "wide",
			width:   120,
			contain: []string{"U@U", "dump", "restore", "clone", "cockpit"},
		},
		{
			name:    "medium",
			width:   65,
			contain: []string{"U@U", "dump", "restore", "history restore"},
		},
		{
			name:    "narrow",
			width:   50,
			contain: []string{"U@U", "dump|restore", "F1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(RenderCapabilitiesStrip(tt.width))
			if len([]rune(got)) > tt.width {
				t.Fatalf("strip longer than width: got %d runes, width %d", len([]rune(got)), tt.width)
			}
			for _, sub := range tt.contain {
				if !strings.Contains(got, sub) {
					t.Fatalf("strip %q missing %q", got, sub)
				}
			}
		})
	}
}

func TestRenderCLIHelpRequiredAndSubsetLabels(t *testing.T) {
	dump := CLICatalog()[0]
	got := stripANSI(RenderCLIHelp(dump, 80))
	if strings.Contains(got, "--dsn*") || strings.Contains(got, "--output*") {
		t.Fatalf("dump alternatives must not be marked required: %s", got)
	}
	if !strings.Contains(got, "config dump.output_dir") || !strings.Contains(got, "--connection") {
		t.Fatalf("missing dump alternative guidance: %s", got)
	}
	if !strings.Contains(got, "CLI-only with --seed-f") || !strings.Contains(got, "--percent") {
		t.Fatalf("missing subset-only label: %s", got)
	}
	for _, flag := range []string{"--slow-connection", "--chunk-size", "--retry-max", "--retry-base"} {
		if !strings.Contains(got, flag) {
			t.Fatalf("missing slow connection flag %q: %s", flag, got)
		}
	}

	restore := CLICatalog()[1]
	gotRestore := stripANSI(RenderCLIHelp(restore, 80))
	if strings.Contains(gotRestore, "--dsn*") {
		t.Fatalf("restore dsn alternative must not be marked required: %s", gotRestore)
	}
	if !strings.Contains(gotRestore, "TUI history restore") {
		t.Fatalf("restore should mention TUI history restore: %s", gotRestore)
	}
	if !strings.Contains(gotRestore, "dolly restore") {
		t.Fatalf("missing restore example: %s", gotRestore)
	}

	clone := catalogCommand("clone")
	gotClone := stripANSI(RenderCLIHelp(clone, 80))
	if strings.Contains(gotClone, "shell only") {
		t.Fatalf("clone should not be shell-only labeled: %s", gotClone)
	}
	if clone.ShellPolicy != ShellPolicyInTUI {
		t.Fatalf("clone ShellPolicy = %v, want InTUI", clone.ShellPolicy)
	}
	if !strings.Contains(gotClone, "dolly clone") {
		t.Fatalf("missing dolly clone example: %s", gotClone)
	}
	if !strings.Contains(gotClone, "dolly clone -ff") {
		t.Fatalf("missing dolly clone -ff example: %s", gotClone)
	}
	if !strings.Contains(gotClone, "config.jsonc") {
		t.Fatalf("missing clone config note: %s", gotClone)
	}
}

func catalogCommand(name string) CLICommand {
	for _, cmd := range CLICatalog() {
		if cmd.Name == name {
			return cmd
		}
	}
	panic("catalog missing command: " + name)
}

func TestHelpPageCountAndBindingsPage(t *testing.T) {
	if HelpPageCount() != 5 {
		t.Fatalf("HelpPageCount = %d, want 5", HelpPageCount())
	}
	page0 := stripANSI(RenderHelpPaged(ScreenConnection, DumpStatusIdle, CloneStatusIdle, 0, 80, 24, false))
	if !strings.Contains(page0, "Keyboard Help") {
		t.Fatalf("page 0 missing bindings header: %s", page0)
	}
	if !strings.Contains(page0, "context help") {
		t.Fatalf("page 0 missing context help binding: %s", page0)
	}
	if !strings.Contains(page0, "keyboard shortcuts") {
		t.Fatalf("page 0 missing keys help binding: %s", page0)
	}

	clonePage := HelpPageCount() - 1
	gotClonePage := stripANSI(RenderHelpPaged(ScreenConnection, DumpStatusIdle, CloneStatusIdle, clonePage, 80, 24, false))
	if !strings.Contains(gotClonePage, "dolly clone") {
		t.Fatalf("clone help page %d missing dolly clone: %s", clonePage, gotClonePage)
	}
}

func TestFlagNamesDump(t *testing.T) {
	names := FlagNames("dump")
	want := []string{"dsn", "connection", "output", "schemas", "no-transaction", "seed-file", "percent", "slow-connection", "chunk-size", "retry-max", "retry-base", "max-depth", "max-tables", "max-rows", "max-rows-per-table", "max-in-list-size", "include-table", "exclude-table", "include-table-file", "exclude-table-file", "chunk-table", "chunk-table-file", "workers", "json"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], w)
		}
	}
}
