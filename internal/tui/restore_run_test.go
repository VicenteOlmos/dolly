package tui

import "testing"

func TestCycleRestoreOnConflict(t *testing.T) {
	if cycleRestoreOnConflict("") != "skip" {
		t.Fatal("empty should cycle to skip")
	}
	if cycleRestoreOnConflict("skip") != "upsert" {
		t.Fatal("skip should cycle to upsert")
	}
	if cycleRestoreOnConflict("upsert") != "error" {
		t.Fatal("upsert should cycle to error")
	}
}
