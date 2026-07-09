package connections

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PickPrompt lists saved profiles on w and reads a selection from r.
func PickPrompt(r io.Reader, w io.Writer, store ConnectionStore) (Connection, error) {
	scanner := bufio.NewScanner(r)
	return PickPromptScanner(scanner, w, store)
}

// PickPromptScanner reads the selection using an existing scanner (e.g. clone prompts).
func PickPromptScanner(scanner *bufio.Scanner, w io.Writer, store ConnectionStore) (Connection, error) {
	if store == nil {
		return Connection{}, fmt.Errorf("connection store is not available")
	}
	list, err := store.List()
	if err != nil {
		return Connection{}, err
	}
	if len(list) == 0 {
		return Connection{}, fmt.Errorf("no saved connections")
	}

	fmt.Fprintln(w, "Saved connections:")
	for i, c := range list {
		fmt.Fprintf(w, "  %d) %s  %s\n", i+1, c.Name, DisplaySummary(c))
	}
	fmt.Fprint(w, "Pick connection (number or name): ")

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Connection{}, fmt.Errorf("prompt cancelled: %w", err)
		}
		return Connection{}, fmt.Errorf("prompt cancelled")
	}
	choice := strings.TrimSpace(scanner.Text())
	if choice == "" {
		return Connection{}, fmt.Errorf("connection selection is required")
	}

	if n, err := strconv.Atoi(choice); err == nil {
		if n < 1 || n > len(list) {
			return Connection{}, fmt.Errorf("invalid selection %d", n)
		}
		return list[n-1], nil
	}

	return store.Get(choice)
}
