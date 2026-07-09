package main

import (
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
	"github.com/VicenteOlmos/dolly/internal/tui"

	tea "charm.land/bubbletea/v2"
)

var isTerminal = func(fd uintptr) bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

var (
	tuiLoadConfig           = config.LoadConfig
	tuiOpenStore            = connections.OpenStore
	tuiLoadDotEnvComponents = config.LoadDotEnvComponents
	tuiRunProgram           = func(app *tui.App) error {
		p := tea.NewProgram(app)
		_, err := p.Run()
		return err
	}
)

func runTUI(args []string) error {
	if wantsHelp(args) {
		printTUIUsage()
		return nil
	}

	if !isTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("tui requires an interactive terminal")
	}

	cfg, err := tuiLoadConfig(config.ResolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	store, err := tuiOpenStore(cfg, cwd)
	if err != nil {
		return fmt.Errorf("open connection store: %w", err)
	}

	cfgPath := config.ResolveConfigPath()
	app := tui.NewAppFromConfig(store, cfg.SaveConnections, cfg, cfgPath)

	if host, port, name, user, password, err := tuiLoadDotEnvComponents(cfg.Env.Path, envNamesFromConfig(cfg)); err == nil {
		app.PrefillConnection(tui.ConnectionDraft{
			Host:     host,
			Port:     port,
			Database: name,
			User:     user,
			Password: password,
		})
	}

	if err := tuiRunProgram(app); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
