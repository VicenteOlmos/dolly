package tui

import (
	"strings"

	"github.com/VicenteOlmos/dolly/internal/brand"
)

// spinnerFrameCount is the cycle length shared by all sheep animations.
const spinnerFrameCount = 8

// cowsayEyeFrames mirrors cowsay -e / -w / -y / -s / -p modes.
var cowsayEyeFrames = []string{"oo", "OO", "..", "@@", "**", "--", "==", "$$"}

var compactSpinnerFrames = []string{
	"U@U~",
	"U@U",
	"~U@U",
	"U@U",
}

func sheepAt(frame int) []string {
	return brand.RenderSheep(cowsayEyeFrames[frame%len(cowsayEyeFrames)])
}

func cloneSheepScene(frame int) []string {
	switch frame % 4 {
	case 0:
		return sheepAt(frame)
	case 1:
		return woolTrailScene(frame)
	case 2:
		return mergeSheepPair(" → ", frame)
	default:
		return mergeSheepPair("   ", frame)
	}
}

func woolTrailScene(frame int) []string {
	lines := sheepAt(frame)
	if len(lines) == 0 {
		return lines
	}
	// Add falling wool on the body line (cowsay @ row).
	for i, line := range lines {
		if strings.Contains(line, "@@") && strings.Contains(line, "\\__/") {
			lines[i] = line + " @··"
			break
		}
	}
	return lines
}

func mergeSheepPair(gap string, frame int) []string {
	left := brand.RenderSheep("oo")
	right := brand.RenderSheep("oo")
	maxH := len(left)
	if len(right) > maxH {
		maxH = len(right)
	}
	out := make([]string, 0, maxH)
	for i := 0; i < maxH; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		switch {
		case l != "" && r != "":
			out = append(out, l+gap+r)
		case l != "":
			out = append(out, l)
		default:
			out = append(out, r)
		}
	}
	return out
}

func formatWalkSpinnerLines(label string, frame int) []string {
	scene := sheepAt(frame)
	lines := make([]string, 0, len(scene)+1)
	for _, line := range scene {
		lines = append(lines, stylePaddedLine(line, StyleSheep))
	}
	lines = append(lines, StyleAccent.Render(label))
	return lines
}

func formatCloneSpinnerLines(label string, frame, width int) []string {
	scene := centerLines(cloneSheepScene(frame), width)
	lines := make([]string, 0, len(scene)+1)
	for _, line := range scene {
		lines = append(lines, stylePaddedLine(line, StyleSheep))
	}
	lines = append(lines, stylePaddedLine(centerLine(label, width), StyleAccent))
	return lines
}

func formatSpinnerCompact(label string, frame int) string {
	glyph := StyleSheep.Render(compactSpinnerFrames[frame%len(compactSpinnerFrames)])
	return glyph + " " + StyleAccent.Render(label)
}

func renderNavBrand() string {
	w := NavContentWidth()
	raw := append(brand.RenderSheep("oo"), brand.Name)
	centered := centerBlock(raw, w)
	var lines []string
	for i, line := range centered {
		if i == len(centered)-1 {
			lines = append(lines, stylePaddedLine(line, StyleBrand))
		} else {
			lines = append(lines, stylePaddedLine(line, StyleSheep))
		}
	}
	return strings.Join(lines, "\n")
}
