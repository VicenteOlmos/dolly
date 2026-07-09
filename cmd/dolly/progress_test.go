package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/VicenteOlmos/dolly/internal/clone"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/restore"
)

func TestRenderTTYSingleRedraw(t *testing.T) {
	var buf bytes.Buffer
	ev := dump.ProgressEvent{
		Phase:   "table_start",
		Table:   "users",
		Current: 1,
		Total:   3,
		Elapsed: 5 * time.Second,
	}

	if err := Render(&buf, ev, true); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("TTY output should start with \\r, got %q", out)
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("TTY output should not contain newline, got %q", out)
	}
	if !strings.Contains(out, "  33%") {
		t.Fatalf("expected 33%%, got %q", out)
	}
	if !strings.Contains(out, "users") {
		t.Fatalf("expected table name, got %q", out)
	}
}

func TestRenderNonTTYPerEventLine(t *testing.T) {
	var buf bytes.Buffer
	ev := restore.ProgressEvent{
		Phase:   "table_start",
		Table:   "orders",
		Current: 2,
		Total:   5,
		Elapsed: 10 * time.Second,
	}

	if err := Render(&buf, ev, false); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if strings.HasPrefix(out, "\r") {
		t.Fatalf("non-TTY output should not start with \\r, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("non-TTY output should end with newline, got %q", out)
	}
	if !strings.Contains(out, "  40%") {
		t.Fatalf("expected 40%%, got %q", out)
	}
	if !strings.Contains(out, "orders") {
		t.Fatalf("expected table name, got %q", out)
	}
}

func TestRenderETADashWhenCurrentLessThanTwo(t *testing.T) {
	var buf bytes.Buffer
	ev := dump.ProgressEvent{
		Phase:   "table_start",
		Table:   "users",
		Current: 1,
		Total:   5,
		Elapsed: 2 * time.Second,
	}

	if err := Render(&buf, ev, false); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ETA —") {
		t.Fatalf("expected ETA — for current<2, got %q", out)
	}
}

func TestRenderETAComputable(t *testing.T) {
	var buf bytes.Buffer
	ev := dump.ProgressEvent{
		Phase:   "table_end",
		Table:   "orders",
		Current: 2,
		Total:   4,
		Elapsed: 10 * time.Second,
	}

	if err := Render(&buf, ev, false); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	// elapsed=10s, current=2, total=4 → ETA = 10*(4-2)/2 = 10s
	if !strings.Contains(out, "ETA 10s") {
		t.Fatalf("expected ETA 10s, got %q", out)
	}
}

func TestRenderCloneEvent(t *testing.T) {
	var buf bytes.Buffer
	ev := clone.ProgressEvent{
		Phase:   "dumping",
		Step:    "dumping schema",
		Table:   "",
		Current: 3,
		Total:   4,
		Elapsed: 15 * time.Second,
	}

	if err := Render(&buf, ev, false); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "  75%") {
		t.Fatalf("expected 75%%, got %q", out)
	}
	// No table name, so phase is used as label
	if !strings.Contains(out, "dumping") {
		t.Fatalf("expected phase label, got %q", out)
	}
}

func TestRenderCompletionSummary(t *testing.T) {
	var buf bytes.Buffer
	ev := dump.ProgressEvent{
		Phase:   "table_end",
		Table:   "users",
		Current: 3,
		Total:   3,
		Elapsed: 30 * time.Second,
	}

	if err := Render(&buf, ev, false); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, " 100%") {
		t.Fatalf("expected 100%% on completion, got %q", out)
	}
	// current >= total → ETA should be dash
	if !strings.Contains(out, "ETA —") {
		t.Fatalf("expected ETA — on completion, got %q", out)
	}
}

func TestRenderUnknownEventNoop(t *testing.T) {
	var buf bytes.Buffer
	// Pass a string — not a known progress event type
	if err := Render(&buf, "not a progress event", false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for unknown event, got %q", buf.String())
	}
}

func TestFormatProgressLineZeroTotal(t *testing.T) {
	pe := progressEvent{Phase: "table_start", Current: 1, Total: 0}
	if got := formatProgressLine(pe); got != "" {
		t.Fatalf("expected empty string for Total=0, got %q", got)
	}
}

func TestFormatCLIDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"sub-second", 500 * time.Millisecond, "<1s"},
		{"seconds", 45 * time.Second, "45s"},
		{"minutes", 2*time.Minute + 30*time.Second, "2m30s"},
		{"hours", 90 * time.Minute, "1h30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCLIDuration(tt.d)
			if got != tt.want {
				t.Fatalf("formatCLIDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestComputeCLIETA(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		current int
		total   int
		wantOK  bool
	}{
		{"current less than 2", 5 * time.Second, 1, 5, false},
		{"zero total", 5 * time.Second, 1, 0, false},
		{"current equals total", 5 * time.Second, 5, 5, false},
		{"current greater than total", 5 * time.Second, 6, 5, false},
		{"valid mid-progress", 10 * time.Second, 2, 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := computeCLIETA(tt.elapsed, tt.current, tt.total)
			if ok != tt.wantOK {
				t.Fatalf("computeCLIETA ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestRenderTTYTableEndPhase(t *testing.T) {
	var buf bytes.Buffer
	ev := dump.ProgressEvent{
		Phase:   "table_end",
		Table:   "accounts",
		Current: 1,
		Total:   1,
		Elapsed: 3 * time.Second,
	}

	if err := Render(&buf, ev, true); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("TTY output should start with \\r, got %q", out)
	}
	if !strings.Contains(out, " 100%") {
		t.Fatalf("expected 100%%, got %q", out)
	}
	if !strings.Contains(out, "accounts") {
		t.Fatalf("expected table name, got %q", out)
	}
}
