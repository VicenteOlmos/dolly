package main

import (
	"flag"
	"sort"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/tui"
)

func flagSetNames(t *testing.T, fs *flag.FlagSet) []string {
	t.Helper()
	var names []string
	fs.VisitAll(func(f *flag.Flag) {
		names = append(names, f.Name)
	})
	sort.Strings(names)
	return names
}

func TestCLICatalogParityDump(t *testing.T) {
	fs := dumpFlagSet(&dumpFlags{})

	got := flagSetNames(t, fs)
	want := tui.FlagNames("dump")
	sort.Strings(want)

	// --slow-connection, --chunk-size, --retry-max, --retry-base are CLI-only.
	withoutCatalog := func(names []string) []string {
		cliOnly := map[string]bool{
			"slow-connection": true,
			"chunk-size":      true,
			"retry-max":       true,
			"retry-base":      true,
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			if cliOnly[n] {
				continue
			}
			out = append(out, n)
		}
		sort.Strings(out)
		return out
	}

	got = withoutCatalog(got)
	if len(got) != len(want) {
		t.Fatalf("dump flags: parser %v catalog %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("dump flag[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCLICatalogParityRestore(t *testing.T) {
	fs := restoreFlagSet(&restoreFlags{})

	got := flagSetNames(t, fs)
	want := tui.FlagNames("restore")
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("restore flags: parser %v catalog %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("restore flag[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCLICatalogParityClone(t *testing.T) {
	var schemasRaw string
	fs := cloneFlagSet(&cloneFlags{}, &schemasRaw)

	got := flagSetNames(t, fs)
	want := tui.FlagNames("clone")
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("clone flags: parser %v catalog %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("clone flag[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
