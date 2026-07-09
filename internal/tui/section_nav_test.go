package tui

import "testing"

func TestSectionNavMoveWraps(t *testing.T) {
	nav := NewSectionNav(3)
	if nav.Section != 0 {
		t.Fatalf("Section = %d, want 0", nav.Section)
	}
	nav.MoveSection(1)
	if nav.Section != 1 {
		t.Fatalf("Section = %d, want 1", nav.Section)
	}
	nav.MoveSection(-1)
	if nav.Section != 0 {
		t.Fatalf("Section = %d, want 0 after move back", nav.Section)
	}
	nav.MoveSection(-1)
	if nav.Section != 2 {
		t.Fatalf("Section = %d, want 2 after wrap backward", nav.Section)
	}
	nav.MoveSection(1)
	if nav.Section != 0 {
		t.Fatalf("Section = %d, want 0 after wrap forward", nav.Section)
	}
}

func TestSectionNavEnterExit(t *testing.T) {
	nav := NewSectionNav(2)
	if !nav.InOverview() {
		t.Fatal("expected overview initially")
	}
	nav.Enter()
	if !nav.InInside() {
		t.Fatal("expected inside after Enter")
	}
	nav.Exit()
	if !nav.InOverview() {
		t.Fatal("expected overview after Esc/Exit")
	}
}

func TestSectionNavEnterInside(t *testing.T) {
	nav := NewSectionNav(3)
	nav.EnterInside(2)
	if !nav.InInside() || nav.Section != 2 {
		t.Fatalf("got level=%v section=%d, want inside section 2", nav.Level, nav.Section)
	}
}
