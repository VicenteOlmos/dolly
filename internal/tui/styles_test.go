package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestInitStylesUsesThemeAttributes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	InitStyles("rose-pine")
	if StyleAccent.GetForeground() == nil {
		t.Fatal("accent style should set foreground from theme")
	}
	if !StyleBrand.GetBold() {
		t.Fatal("brand style should be bold")
	}
	if !StyleAccent.GetBold() {
		t.Fatal("accent style should be bold")
	}
}

func TestNavBrandCentersName(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := renderNavBrand()
	lines := strings.Split(got, "\n")
	if len(lines) == 0 {
		t.Fatal("expected nav brand lines")
	}
	name := stripANSI(lines[len(lines)-1])
	if strings.TrimSpace(name) != "dolly" {
		t.Fatalf("last line should be brand name, got %q", name)
	}
	wantCenter := NavContentWidth() / 2
	for i, line := range lines {
		plain := stripANSI(line)
		pad := leadingSpaces(plain)
		center := pad + lipgloss.Width(strings.TrimSpace(plain))/2
		if center < wantCenter-1 || center > wantCenter+1 {
			t.Fatalf("line %d visual center=%d want ~%d (%q)", i, center, wantCenter, plain)
		}
	}
}
