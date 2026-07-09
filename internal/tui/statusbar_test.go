package tui

import (
	"strings"
	"testing"
)

func TestRenderScreenNavVerticalMenu(t *testing.T) {
	got := stripANSI(RenderScreenNav(20, ScreenDump))
	if strings.Contains(got, "›") {
		t.Fatalf("expected vertical menu, got horizontal breadcrumb: %s", got)
	}
	for _, want := range []string{"> 3 dump", "1 connect", "5 config", "dolly"} {
		if !strings.Contains(got, want) {
			t.Fatalf("nav missing %q: %s", want, got)
		}
	}
}

func TestStatusBarMaxFiveSegments(t *testing.T) {
	cases := []struct {
		screen          Screen
		dump            DumpStatus
		clone           CloneStatus
		saveConnections bool
	}{
		{screen: ScreenConnection, saveConnections: true},
		{screen: ScreenConnection, saveConnections: false},
		{screen: ScreenSchema},
		{screen: ScreenDump},
		{screen: ScreenDump, dump: DumpStatusRunning},
		{screen: ScreenDump, dump: DumpStatusComplete},
		{screen: ScreenDump, dump: DumpStatusError},
		{screen: ScreenClone},
		{screen: ScreenClone, clone: CloneStatusRunning},
		{screen: ScreenClone, clone: CloneStatusComplete},
	}

	for _, tc := range cases {
		hint := defaultStatusHint(tc.screen, tc.dump, tc.clone, tc.saveConnections)
		n := statusHintSegmentCount(hint)
		if n > 5 {
			t.Fatalf("screen %v dump %v clone %v: %d segments in %q", tc.screen, tc.dump, tc.clone, n, hint)
		}
	}
}
