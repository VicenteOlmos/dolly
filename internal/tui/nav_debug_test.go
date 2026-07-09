package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestNavCenterDebug(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	nav := RenderScreenNav(24, ScreenConnection)
	for i, line := range strings.Split(nav, "\n") {
		t.Logf("%2d w=%d %q", i+1, lipgloss.Width(line), line)
	}
	for _, line := range strings.Split(nav, "\n") {
		if strings.Contains(line, "dolly") {
			parts := strings.Split(line, "│")
			if len(parts) >= 3 {
				inner := parts[1]
				trim := strings.TrimSpace(inner)
				padL := strings.Index(inner, trim)
				innerW := lipgloss.Width(inner)
				textW := lipgloss.Width(trim)
				center := padL + textW/2
				want := innerW / 2
				if center < want-1 || center > want+1 {
					t.Fatalf("dolly off-center: center=%d want=%d inner=%q", center, want, inner)
				}
			}
		}
	}
	fmt.Println("ok")
}
