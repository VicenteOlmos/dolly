package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestStylePaddedLineKeepsPaddingOutsideANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	line := "     dolly     "
	got := stylePaddedLine(line, StyleBrand)
	want := "     " + StyleBrand.Render("dolly") + "     "
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCenterBlockSharesVisualCenter(t *testing.T) {
	lines := []string{
		"   __",
		"  UooU",
		"  long line",
	}
	width := 20
	got := centerBlock(lines, width)
	wantCenter := width / 2
	for i, line := range got {
		pad := leadingSpaces(line)
		center := pad + lipgloss.Width(strings.TrimSpace(line))/2
		if center < wantCenter-1 || center > wantCenter+1 {
			t.Fatalf("line %d center=%d want ~%d line %q", i, center, wantCenter, line)
		}
	}
}

func TestRenderScreenNavBrandVisuallyCentered(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	nav := RenderScreenNav(24, ScreenConnection)
	assertNavRowCentered(t, nav, "__")
	assertNavRowCentered(t, nav, "dolly")
}

func assertNavRowCentered(t *testing.T, nav, needle string) {
	t.Helper()
	for _, line := range strings.Split(nav, "\n") {
		if !strings.Contains(stripANSI(line), needle) {
			continue
		}
		parts := strings.Split(line, "│")
		if len(parts) < 3 {
			t.Fatalf("unexpected nav row: %q", line)
		}
		inner := stripANSI(parts[1])
		trim := strings.TrimSpace(inner)
		padL := strings.Index(inner, trim)
		innerW := lipgloss.Width(inner)
		textW := lipgloss.Width(trim)
		center := padL + textW/2
		want := innerW / 2
		if center < want-1 || center > want+1 {
			t.Fatalf("%q off-center: center=%d want=%d row=%q", needle, center, want, stripANSI(line))
		}
		return
	}
	t.Fatalf("row containing %q not found in nav", needle)
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}
