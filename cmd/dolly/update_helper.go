package main

import (
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/update"
)

func runUpdateHelper(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: dolly __update-helper <manifest> <capability>")
	}
	return update.RunHelper(args[0], args[1])
}

func runUpdateCleanup(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: dolly __update-cleanup <manifest> <capability>")
	}
	return update.RunCleanup(args[0], args[1])
}

func dispatchUpdateInternal(args []string) (bool, int) {
	if len(args) < 2 {
		return false, 0
	}
	switch args[1] {
	case "__update-helper":
		if err := runUpdateHelper(args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return true, 1
		}
		return true, 0
	case "__update-cleanup":
		if err := runUpdateCleanup(args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return true, 1
		}
		return true, 0
	default:
		return false, 0
	}
}
