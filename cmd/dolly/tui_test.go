package main

import (
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/tui"
)

func TestRunTUINonTTY(t *testing.T) {
	orig := isTerminal
	t.Cleanup(func() { isTerminal = orig })
	isTerminal = func(uintptr) bool { return false }

	err := runTUI(nil)
	if err == nil {
		t.Fatal("expected error for non-TTY")
	}
	if !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("error = %q, want interactive terminal message", err.Error())
	}
}

func TestRunTUIHelpNonTTY(t *testing.T) {
	orig := isTerminal
	t.Cleanup(func() { isTerminal = orig })
	isTerminal = func(uintptr) bool { return false }

	out := captureStderr(func() {
		err := runTUI([]string{"-h"})
		if err != nil {
			t.Fatalf("runTUI help: %v", err)
		}
	})
	if !strings.Contains(out, "usage: dolly tui") {
		t.Fatalf("tui usage missing:\n%s", out)
	}
}

func TestRunTUIPrepopulatesConnFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	origTerminal := isTerminal
	origLoadConfig := tuiLoadConfig
	origOpenStore := tuiOpenStore
	origLoadComponents := tuiLoadDotEnvComponents
	origRunProgram := tuiRunProgram
	t.Cleanup(func() {
		isTerminal = origTerminal
		tuiLoadConfig = origLoadConfig
		tuiOpenStore = origOpenStore
		tuiLoadDotEnvComponents = origLoadComponents
		tuiRunProgram = origRunProgram
	})

	isTerminal = func(uintptr) bool { return true }
	tuiLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	tuiOpenStore = connections.OpenStore
	tuiLoadDotEnvComponents = func(_ string, _ config.EnvVarNames) (string, string, string, string, string, error) {
		return "envhost", "5433", "envdb", "envuser", "envpass", nil
	}

	var captured *tui.App
	tuiRunProgram = func(app *tui.App) error {
		captured = app
		return nil
	}

	if err := runTUI(nil); err != nil {
		t.Fatalf("runTUI: %v", err)
	}
	if captured == nil {
		t.Fatal("expected app passed to tuiRunProgram")
	}

	draft := captured.ConnectionDraft()
	if draft.Host != "envhost" || draft.Port != "5433" || draft.Database != "envdb" ||
		draft.User != "envuser" || draft.Password != "envpass" {
		t.Fatalf("conn draft = %+v, want env values", draft)
	}
}

func TestRunTUIMissingDotEnvLeavesConnEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	origTerminal := isTerminal
	origLoadConfig := tuiLoadConfig
	origOpenStore := tuiOpenStore
	origLoadComponents := tuiLoadDotEnvComponents
	origRunProgram := tuiRunProgram
	t.Cleanup(func() {
		isTerminal = origTerminal
		tuiLoadConfig = origLoadConfig
		tuiOpenStore = origOpenStore
		tuiLoadDotEnvComponents = origLoadComponents
		tuiRunProgram = origRunProgram
	})

	isTerminal = func(uintptr) bool { return true }
	tuiLoadConfig = func(string) (*config.Config, error) {
		return config.DefaultConfig(), nil
	}
	tuiOpenStore = connections.OpenStore
	tuiLoadDotEnvComponents = func(_ string, _ config.EnvVarNames) (string, string, string, string, string, error) {
		return "", "", "", "", "", config.ErrSourceDSNNotFound
	}

	var captured *tui.App
	tuiRunProgram = func(app *tui.App) error {
		captured = app
		return nil
	}

	if err := runTUI(nil); err != nil {
		t.Fatalf("runTUI: %v", err)
	}
	if captured == nil {
		t.Fatal("expected app passed to tuiRunProgram")
	}
	if draft := captured.ConnectionDraft(); draft != (tui.ConnectionDraft{}) {
		t.Fatalf("conn draft = %+v, want zero value", draft)
	}
}
