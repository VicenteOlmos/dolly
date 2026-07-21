package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

func printConfigUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly config <subcommand>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "subcommands:")
	fmt.Fprintln(os.Stderr, "  init        write default config.jsonc (no-op if file already exists)")
	fmt.Fprintln(os.Stderr, "  show        print resolved config as JSON to stdout")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --force     overwrite existing config.jsonc (init only)")
}

func runConfigInit(args []string) error {
	if wantsHelp(args) {
		printConfigUsage()
		return nil
	}

	force := false
	for _, a := range args {
		switch a {
		case "--force", "-force":
			force = true
		default:
			return fmt.Errorf("unknown config init flag %q", a)
		}
	}

	const path = "config.jsonc"

	if !force {
		if _, err := os.Stat(path); err == nil {
			return errors.New("config.jsonc already exists; use --force to overwrite")
		}
	}

	if force {
		if err := config.WriteConfigFile(path, config.DefaultTemplate()); err != nil {
			return fmt.Errorf("write config.jsonc: %w", err)
		}
		fmt.Fprintln(os.Stderr, "config.jsonc written (overwritten)")
		return nil
	}

	// No existing file — use BootstrapConfig.
	if err := config.BootstrapConfig(path); err != nil {
		return fmt.Errorf("init config: %w", err)
	}
	fmt.Fprintln(os.Stderr, "config.jsonc written")
	return nil
}

func runConfigShow(args []string) error {
	if wantsHelp(args) {
		printConfigUsage()
		return nil
	}
	path := config.ResolveConfigPath()
	cfg, err := config.LoadConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Clone.TargetURL = connections.RedactDSN(cfg.Clone.TargetURL)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func runConfig(args []string) error {
	if len(args) == 0 || wantsHelp(args) {
		printConfigUsage()
		if len(args) == 0 {
			return errors.New("config requires a subcommand")
		}
		return nil
	}

	switch args[0] {
	case "init":
		return runConfigInit(args[1:])
	case "show":
		return runConfigShow(args[1:])
	default:
		printConfigUsage()
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}
