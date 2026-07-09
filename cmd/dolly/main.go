package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/connections"
)

var errJSONHandled = errors.New("json error already emitted to stderr")

// emitJSONError writes a JSON error envelope to w (stderr) when --json is active.
func emitJSONError(w io.Writer, command, message string) {
	b, _ := json.MarshalIndent(map[string]any{
		"ok":      false,
		"command": command,
		"error":   connections.RedactMessage(message),
	}, "", "  ")
	fmt.Fprintln(w, string(b))
}

func main() {
	os.Exit(dispatch(os.Args))
}

func dispatch(args []string) int {
	if len(args) < 2 {
		printRootUsage()
		return 1
	}

	if args[1] == "-h" || args[1] == "--help" {
		printRootUsage()
		return 0
	}

	if args[1] == "--version" || args[1] == "version" {
		if err := runVersion(args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	// Best-effort bootstrap: write config.jsonc once if no config file exists.
	// Non-fatal — defaults still apply when the write fails or the file exists.
	_ = config.BootstrapConfig(config.ResolveConfigPath())

	switch args[1] {
	case "dump":
		if len(args) >= 3 && isDumpListInvocation(args[2:]) {
			if err := runDumpList(args[3:]); err != nil {
				if !errors.Is(err, errJSONHandled) {
					fmt.Fprintln(os.Stderr, err)
				}
				return 1
			}
		} else if err := runDump(args[2:]); err != nil {
			if !errors.Is(err, errJSONHandled) {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
	case "restore":
		if err := runRestore(args[2:]); err != nil {
			if !errors.Is(err, errJSONHandled) {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
	case "tui":
		if err := runTUI(args[2:]); err != nil {
			if !errors.Is(err, errJSONHandled) {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
	case "clone":
		if err := runClone(args[2:]); err != nil {
			if !errors.Is(err, errJSONHandled) {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
	case "config":
		if err := runConfig(args[2:]); err != nil {
			if !errors.Is(err, errJSONHandled) {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command")
		return 1
	}
	return 0
}
