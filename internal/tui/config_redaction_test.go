package tui

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestConfigScreenRedactsTargetURLCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.TargetURL = "postgres://user:secret@db.example/app?sslmode=require&token=hidden"
	screen := newConfigScreen(func() *config.Config { return cfg }, func() string { return "config.jsonc" })
	view := screen.View(120, 80)
	if strings.Contains(view, "secret") || strings.Contains(view, "hidden") {
		t.Fatalf("config screen leaked credential: %s", view)
	}
	if !strings.Contains(view, "db.example") || !strings.Contains(view, "sslmode=require") {
		t.Fatalf("config screen lost usable target details: %s", view)
	}
}

func TestConfigScreenMasksTargetURLWhileEditing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Clone.TargetURL = "postgres://user:secret@db.example/app?sslmode=require&token=hidden"
	cs := newConfigScreen(func() *config.Config { return cfg }, func() string { return "config.jsonc" }).(*configScreen)
	for i, field := range cs.fields {
		if field.Section == "clone" && field.Label == "target_url" {
			cs.cursor = i
			break
		}
	}
	cs.handleEnter()
	view := cs.View(120, 80)
	if strings.Contains(view, "secret") || strings.Contains(view, "hidden") {
		t.Fatalf("editing screen leaked credential: %s", view)
	}
	if cs.editValue != cfg.Clone.TargetURL {
		t.Fatalf("editable value = %q, want original DSN", cs.editValue)
	}
}

func TestCloneScreenMasksManualTargetDSN(t *testing.T) {
	draft := &CloneDraft{TargetSource: TargetSourceManual, TargetDSN: "postgres://user:secret@db.example/app?token=hidden"}
	status, cloneErr, spinner := CloneStatusIdle, "", 0
	log := []string{}
	cs := newCloneScreen(draft, func() bool { return true }, &status, &log, &cloneErr, &spinner, nil, false, config.DefaultConfig, nil, nil).(*cloneScreen)
	cs.nav.EnterInside(cloneSectionForm)
	cs.formField = 1
	for _, focused := range []bool{false, true} {
		if !focused {
			cs.nav.Exit()
		}
		view := cs.renderTargetField(120)
		if strings.Contains(view, "secret") || strings.Contains(view, "hidden") {
			t.Fatalf("manual target DSN leaked while focused=%v: %s", focused, view)
		}
		if cs.draft.TargetDSN != "postgres://user:secret@db.example/app?token=hidden" {
			t.Fatalf("manual target DSN was mutated: %q", cs.draft.TargetDSN)
		}
		cs.nav.EnterInside(cloneSectionForm)
	}
}
