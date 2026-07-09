package tui

import (
	"charm.land/lipgloss/v2"
)

// Package-level styles; reassigned by InitStyles when the config theme changes.
var (
	StyleBase              lipgloss.Style
	StyleMuted             lipgloss.Style
	StyleAccent            lipgloss.Style
	StyleSheep             lipgloss.Style
	StyleBrand             lipgloss.Style
	StyleBorder            lipgloss.Style
	StyleHeader            lipgloss.Style
	StyleStatusBar         lipgloss.Style
	StyleHelpOverlay       lipgloss.Style
	StyleWarning           lipgloss.Style
	StyleCapabilitiesStrip lipgloss.Style
	StyleModal             lipgloss.Style
	StyleHelpPanel         lipgloss.Style
	StyleProgressBar       lipgloss.Style
	StyleProgressTrack     lipgloss.Style
)

func init() {
	InitStyles(DefaultTheme)
}

// InitStyles applies a named palette from config (see themes.go). Backgrounds stay
// unset so the terminal wallpaper/theme still shows between panels.
func InitStyles(theme string) {
	p := lookupPalette(theme)
	text := lipgloss.Color(p.Text)
	muted := lipgloss.Color(p.Muted)
	accent := lipgloss.Color(p.Accent)
	border := lipgloss.Color(p.Border)
	warning := lipgloss.Color(p.Warning)
	success := lipgloss.Color(p.Success)

	StyleBase = lipgloss.NewStyle().Foreground(text)
	StyleMuted = lipgloss.NewStyle().Foreground(muted)
	StyleAccent = lipgloss.NewStyle().Foreground(accent).Bold(true)
	StyleSheep = lipgloss.NewStyle().Foreground(text)
	StyleBrand = lipgloss.NewStyle().Foreground(accent).Bold(true)
	StyleHeader = lipgloss.NewStyle().Foreground(accent).Bold(true)
	StyleBorder = lipgloss.NewStyle().
		Foreground(border).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	StyleStatusBar = lipgloss.NewStyle().Foreground(muted)
	StyleHelpOverlay = lipgloss.NewStyle().
		Foreground(text).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	StyleWarning = lipgloss.NewStyle().
		Foreground(warning).
		Bold(true).
		Underline(true)
	StyleCapabilitiesStrip = lipgloss.NewStyle().Foreground(muted)
	StyleModal = lipgloss.NewStyle().
		Foreground(text).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	StyleHelpPanel = lipgloss.NewStyle().
		Foreground(text).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1)
	StyleProgressBar = lipgloss.NewStyle().Foreground(success).Bold(true)
	StyleProgressTrack = lipgloss.NewStyle().Foreground(border)
}
