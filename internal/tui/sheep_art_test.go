package tui

import (
	"strings"
	"testing"
)

func TestCloneSheepSceneTwinHasTwoFaces(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lines := cloneSheepScene(3)
	block := strings.Join(lines, "\n")
	if strings.Count(block, "UooU") < 2 {
		t.Fatalf("expected twin cowsay sheep, got:\n%s", block)
	}
}

func TestFormatCloneSpinnerLinesShowsWool(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	lines := formatCloneSpinnerLines("Cloning…", 3, 70)
	block := strings.Join(lines, "\n")
	if !strings.Contains(block, "@@") {
		t.Fatalf("expected cowsay wool, got:\n%s", block)
	}
}

func TestRenderNavBrandUsesCowsaySheep(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := renderNavBrand()
	if !strings.Contains(got, "dolly") {
		t.Fatalf("expected brand name, got %q", got)
	}
	if !strings.Contains(got, "UooU") {
		t.Fatalf("expected cowsay face, got %q", got)
	}
	if !strings.Contains(got, "YY") {
		t.Fatalf("expected cowsay legs, got %q", got)
	}
}

func TestSheepAtUsesCowsayEyeModes(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := strings.Join(sheepAt(1), "\n")
	if !strings.Contains(got, "OO") {
		t.Fatalf("expected wired eyes frame (cowsay -w), got %q", got)
	}
}
