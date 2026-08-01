package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/update"
)

type updateFlags struct {
	check bool
	json  bool
}

func printUpdateUsage() {
	fmt.Fprintln(os.Stderr, "usage: dolly update [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  --check   discover and verify a newer release without replacing the executable")
	fmt.Fprintln(os.Stderr, "  --json    emit machine-readable JSON result to stdout (errors to stderr)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Installs the latest stable GitHub release when a newer version is available.")
	fmt.Fprintln(os.Stderr, "On Windows the running executable may remain locked; replacement is deferred to a hidden helper.")
	fmt.Fprintln(os.Stderr, "Development builds and prereleases are not supported. No force or downgrade.")
}

func updateFlagSet(flags *updateFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.BoolVar(&flags.check, "check", false, "discover and verify without replacing")
	fs.BoolVar(&flags.json, "json", false, "emit JSON result")
	return fs
}

func parseUpdateFlags(args []string) (updateFlags, error) {
	if wantsHelp(args) {
		printUpdateUsage()
		return updateFlags{}, errHelp
	}
	var flags updateFlags
	fs := updateFlagSet(&flags)
	if err := fs.Parse(args); err != nil {
		return updateFlags{}, err
	}
	if fs.NArg() > 0 {
		return updateFlags{}, fmt.Errorf("unknown update argument %q", fs.Arg(0))
	}
	return flags, nil
}

func emitUpdateJSON(result *update.Result, runErr error) error {
	if result == nil {
		emitJSONError(os.Stderr, "update", "update failed")
		return errJSONHandled
	}
	if !result.OK {
		payload := map[string]any{
			"ok":      false,
			"command": "update",
			"status":  string(update.StatusFailed),
			"error":   result.Error,
		}
		b, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Fprintln(os.Stderr, string(b))
		return errJSONHandled
	}

	payload := map[string]any{
		"ok":                true,
		"command":           "update",
		"status":            string(result.Status),
		"installed_version": result.InstalledVersion,
		"remote_version":    result.RemoteVersion,
		"asset":             result.Asset,
		"target":            result.Target,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal update result: %w", err)
	}
	fmt.Println(string(b))
	if runErr != nil {
		return runErr
	}
	return nil
}

func emitUpdateText(result *update.Result, runErr error) error {
	if result == nil || !result.OK {
		msg := "update failed"
		if result != nil && result.Error != "" {
			msg = result.Error
		} else if runErr != nil {
			msg = runErr.Error()
		}
		fmt.Fprintln(os.Stderr, msg)
		return errTextHandled
	}

	switch result.Status {
	case update.StatusCurrent:
		fmt.Printf("dolly is up to date (%s)\n", result.InstalledVersion)
	case update.StatusAvailable:
		if result.RemoteVersion != "" {
			fmt.Printf("update available: %s -> %s (%s)\n", result.InstalledVersion, result.RemoteVersion, result.Asset)
		} else {
			fmt.Printf("update available (%s)\n", result.Asset)
		}
	case update.StatusUpdated:
		fmt.Printf("updated dolly to %s\n", result.RemoteVersion)
	case update.StatusDeferred:
		fmt.Printf("update deferred: helper will replace %s after exit (%s -> %s)\n", result.Target, result.InstalledVersion, result.RemoteVersion)
	default:
		fmt.Printf("update status: %s\n", result.Status)
	}
	return runErr
}
