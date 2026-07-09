package main

import (
	"encoding/json"
	"fmt"
)

var (
	version = "dev"
	commit  = "dev"
	date    = "unknown"
)

func printVersion() {
	fmt.Printf("dolly %s (commit %s, built %s)\n", version, commit, date)
}

func runVersion(args []string) error {
	if wantsHelp(args) {
		printVersionUsage()
		return nil
	}
	jsonMode := false
	for _, a := range args {
		if a == "--json" {
			jsonMode = true
			break
		}
	}
	if !jsonMode {
		printVersion()
		return nil
	}
	result := map[string]any{
		"ok":      true,
		"command": "version",
		"version": version,
		"commit":  commit,
		"date":    date,
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal version: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
