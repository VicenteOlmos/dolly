package tui

import "strings"

// DefaultTheme is used when config omits tui.theme or names an unknown palette.
const DefaultTheme = "catppuccin-mocha"

// palette holds lipgloss hex colors for one TUI theme.
type palette struct {
	Text    string
	Muted   string
	Accent  string
	Border  string
	Warning string
	Success string
}

var themeCatalog = map[string]palette{
	"catppuccin-mocha": {
		Text: "#cdd6f4", Muted: "#a6adc8", Accent: "#b4befe", Border: "#6c7086",
		Warning: "#f38ba8", Success: "#a6e3a1",
	},
	"catppuccin-frappe": {
		Text: "#c6d0f5", Muted: "#b5bfe2", Accent: "#babbf1", Border: "#737994",
		Warning: "#e78284", Success: "#a6d189",
	},
	"catppuccin-latte": {
		Text: "#4c4f69", Muted: "#6c6f85", Accent: "#7287fd", Border: "#9ca0b0",
		Warning: "#d20f39", Success: "#40a02b",
	},
	"rose-pine": {
		Text: "#e0def4", Muted: "#908caa", Accent: "#c4a7e7", Border: "#6e6a86",
		Warning: "#eb6f92", Success: "#9ccfd8",
	},
	"rose-pine-moon": {
		Text: "#e0def4", Muted: "#908caa", Accent: "#c4a7e7", Border: "#6e6a86",
		Warning: "#eb6f92", Success: "#9ccfd8",
	},
	"rose-pine-dawn": {
		Text: "#575279", Muted: "#797593", Accent: "#907aa9", Border: "#9893a5",
		Warning: "#b4637a", Success: "#56949f",
	},
	"dracula": {
		Text: "#f8f8f2", Muted: "#6272a4", Accent: "#bd93f9", Border: "#44475a",
		Warning: "#ff5555", Success: "#50fa7b",
	},
	"gruvbox-dark": {
		Text: "#ebdbb2", Muted: "#a89984", Accent: "#d3869b", Border: "#665c54",
		Warning: "#fb4934", Success: "#b8bb26",
	},
	"gruvbox-light": {
		Text: "#3c3836", Muted: "#7c6f64", Accent: "#8f3f71", Border: "#bdae93",
		Warning: "#9d0006", Success: "#79740e",
	},
	"tokyo-night": {
		Text: "#c0caf5", Muted: "#565f89", Accent: "#7aa2f7", Border: "#414868",
		Warning: "#f7768e", Success: "#9ece6a",
	},
	"nord": {
		Text: "#eceff4", Muted: "#81a1c1", Accent: "#88c0d0", Border: "#4c566a",
		Warning: "#bf616a", Success: "#a3be8c",
	},
	"one-dark": {
		Text: "#abb2bf", Muted: "#5c6370", Accent: "#c678dd", Border: "#4b5263",
		Warning: "#e06c75", Success: "#98c379",
	},
	"everforest": {
		Text: "#d3c6aa", Muted: "#a89984", Accent: "#7fbbb3", Border: "#5c6a72",
		Warning: "#e67e80", Success: "#a7c080",
	},
	"github-dark": {
		Text: "#c9d1d9", Muted: "#8b949e", Accent: "#79c0ff", Border: "#484f58",
		Warning: "#ff7b72", Success: "#3fb950",
	},
}

// ThemeNames returns supported theme ids in stable display order.
func ThemeNames() []string {
	return []string{
		"catppuccin-mocha",
		"catppuccin-frappe",
		"catppuccin-latte",
		"rose-pine",
		"rose-pine-moon",
		"rose-pine-dawn",
		"dracula",
		"gruvbox-dark",
		"gruvbox-light",
		"tokyo-night",
		"nord",
		"one-dark",
		"everforest",
		"github-dark",
	}
}

// NormalizeTheme maps config input to a catalog key; unknown values fall back to DefaultTheme.
func NormalizeTheme(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return DefaultTheme
	}
	key = strings.ReplaceAll(key, "_", "-")
	if _, ok := themeCatalog[key]; ok {
		return key
	}
	// Common aliases / Ghostty theme file stems.
	switch key {
	case "catppuccin", "mocha":
		return "catppuccin-mocha"
	case "frappe":
		return "catppuccin-frappe"
	case "latte":
		return "catppuccin-latte"
	case "rosepine", "rose-pine-main":
		return "rose-pine"
	case "gruvbox":
		return "gruvbox-dark"
	case "tokyonight", "tokyo-night-night":
		return "tokyo-night"
	case "onedark", "one-dark-pro":
		return "one-dark"
	case "github":
		return "github-dark"
	}
	return DefaultTheme
}

func lookupPalette(name string) palette {
	if p, ok := themeCatalog[NormalizeTheme(name)]; ok {
		return p
	}
	return themeCatalog[DefaultTheme]
}
