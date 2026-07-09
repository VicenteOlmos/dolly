package tui

import (
	"fmt"
	"strings"
)

// ScreenNavWidth is the total column width of the vertical workflow menu (border included).
const ScreenNavWidth = 32

var screenNavLabels = []string{"connect", "schema", "dump", "clone", "config"}

func RenderScreenNav(height int, active Screen) string {
	var lines []string
	lines = append(lines, renderNavBrand())
	lines = append(lines, "")
	for i, label := range screenNavLabels {
		row := fmt.Sprintf("%d %s", i+1, label)
		if Screen(i) == active {
			row = StyleAccent.Render("> " + row)
		} else {
			row = StyleMuted.Render("  " + row)
		}
		lines = append(lines, row)
	}
	content := strings.Join(lines, "\n")
	innerH := max(0, height-2)
	return StyleBorder.Width(max(0, ScreenNavWidth-2)).Height(innerH).Render(content)
}
