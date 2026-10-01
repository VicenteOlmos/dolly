package clone

import (
	"strings"
	"testing"
)

func TestColumnStatisticsTargetQueryFilter(t *testing.T) {
	t.Parallel()
	q := columnStatisticsTargetQuery("$1")
	for _, fragment := range []string{
		"a.attstattarget > 0",
		"NOT a.attisdropped",
		"a.attnum > 0",
		"c.relkind IN ('r', 'p')",
	} {
		if !strings.Contains(q, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, q)
		}
	}
}
