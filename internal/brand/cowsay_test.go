package brand

import (
	"strings"
	"testing"
)

func TestRenderSheepMatchesCowsayShape(t *testing.T) {
	lines := RenderSheep("oo")
	if len(lines) < 5 {
		t.Fatalf("expected cowsay sheep body, got %d lines: %q", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"__", "UooU", "@@", "YY", "||"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("sheep missing %q in:\n%s", want, joined)
		}
	}
}

func TestRenderSheepCustomEyes(t *testing.T) {
	lines := RenderSheep("..")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "U..U") {
		t.Fatalf("expected youthful eyes, got:\n%s", joined)
	}
}

func TestRenderSheepTrimsThoughtLines(t *testing.T) {
	lines := RenderSheep("oo")
	if strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("first line should be sheep body, not blank: %q", lines)
	}
}

func TestRenderSheepGoldenSnapshot(t *testing.T) {
	lines := RenderSheep("oo")
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "\\__") {
		t.Fatalf("expected cowsay \\__/ wool row, got:\n%s", joined)
	}
}
