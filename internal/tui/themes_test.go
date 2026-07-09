package tui

import (
	"testing"

	"github.com/lucasb-eyer/go-colorful"
)

func TestNormalizeThemeAliases(t *testing.T) {
	tests := map[string]string{
		"":                    DefaultTheme,
		"  Catppuccin-Mocha ": "catppuccin-mocha",
		"rosepine":            "rose-pine",
		"gruvbox":             "gruvbox-dark",
		"unknown-theme":       DefaultTheme,
	}
	for in, want := range tests {
		if got := NormalizeTheme(in); got != want {
			t.Fatalf("NormalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThemeNamesIncludesDefault(t *testing.T) {
	names := ThemeNames()
	if len(names) < 10 {
		t.Fatalf("expected many themes, got %d", len(names))
	}
	if names[0] != DefaultTheme {
		t.Fatalf("first theme = %q, want %q", names[0], DefaultTheme)
	}
}

func TestInitStylesAppliesPalette(t *testing.T) {
	InitStyles("dracula")
	c, ok := colorful.MakeColor(StyleAccent.GetForeground())
	if !ok || c.Hex() != "#bd93f9" {
		t.Fatalf("dracula accent = %v, want #bd93f9", StyleAccent.GetForeground())
	}
	InitStyles(DefaultTheme)
}
