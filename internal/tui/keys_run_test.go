package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRunKeyTrigger(t *testing.T) {
	tests := []struct {
		name        string
		key         tea.KeyPressMsg
		want        bool
		wantLetterG bool
	}{
		{"ctrl+enter", keyPress("", tea.KeyEnter, tea.ModCtrl), true, false},
		{"f5 code", keyPress("", tea.KeyF5, 0), true, false},
		{"g", keyPress("g", 'g', 0), true, true},
		{"G shifted", keyPress("G", 'G', tea.ModShift), false, false},
		{"enter plain", keyPress("", tea.KeyEnter, 0), false, false},
		{"a", keyPress("a", 'a', 0), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, isG := runKeyTrigger(tt.key)
			if got != tt.want || isG != tt.wantLetterG {
				t.Fatalf("runKeyTrigger() = (%v, %v), want (%v, %v)", got, isG, tt.want, tt.wantLetterG)
			}
		})
	}
}
