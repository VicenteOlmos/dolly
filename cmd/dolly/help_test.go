package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestWantsHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"short help", []string{"-h"}, true},
		{"long help", []string{"--help"}, true},
		{"help among flags", []string{"--dsn", "x", "--help"}, true},
		{"short among flags", []string{"-h", "--dsn", "x"}, true},
		{"no help", []string{"--dsn", "postgres://h-x/db_x"}, false},
		{"empty", nil, false},
		{"similar not help", []string{"--helpful"}, false},
		{"hostname false positive", []string{"-host"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wantsHelp(tt.args); got != tt.want {
				t.Fatalf("wantsHelp(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestPrintRootUsage(t *testing.T) {
	out := captureStderr(printRootUsage)
	for _, sub := range []string{
		"usage: dolly <command>",
		"dump",
		"restore",
		"clone",
		"tui",
		"config",
		"dolly <command> --help",
	} {
		if !strings.Contains(out, sub) {
			t.Fatalf("root usage missing %q:\n%s", sub, out)
		}
	}
}

func TestPrintDumpUsage(t *testing.T) {
	out := captureStderr(printDumpUsage)
	for _, sub := range []string{
		"--dsn",
		"--connection",
		"--output",
		"--schemas",
		"dump.schemas",
		"dolly dump list",
		"--no-transaction",
		"--slow-connection",
		"--seed-file",
		"--max-depth",
		"default 10",
	} {
		if !strings.Contains(out, sub) {
			t.Fatalf("dump usage missing %q:\n%s", sub, out)
		}
	}
}

func TestPrintRestoreUsage(t *testing.T) {
	out := captureStderr(printRestoreUsage)
	for _, sub := range []string{"--dsn", "--connection", "--input", "--on-conflict", "--replace", "--no-transaction", "advanced", "--trust-schema-sql", "--yes", "default is atomic"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("restore usage missing %q:\n%s", sub, out)
		}
	}
}

func TestPrintCloneUsage(t *testing.T) {
	out := captureStderr(printCloneUsage)
	for _, sub := range []string{"--ff", "--strategy", "--connection", "--schemas", "clone.schemas", "sanitization.enabled", "config.jsonc"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("clone usage missing %q:\n%s", sub, out)
		}
	}
}

func TestPrintTUIUsage(t *testing.T) {
	out := captureStderr(printTUIUsage)
	for _, sub := range []string{"usage: dolly tui", "interactive", "config.jsonc"} {
		if !strings.Contains(out, sub) {
			t.Fatalf("tui usage missing %q:\n%s", sub, out)
		}
	}
}

func TestParseDumpListFlagsHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out := captureStderr(func() {
				_, err := parseDumpListFlags(args)
				if !errors.Is(err, errHelp) {
					t.Fatalf("err = %v, want errHelp", err)
				}
			})
			if !strings.Contains(out, "dolly dump list") || !strings.Contains(out, "--json") {
				t.Fatalf("dump list usage missing expected flags:\n%s", out)
			}
		})
	}
}

func TestParseDumpFlagsHelp(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out := captureStderr(func() {
				_, err := parseDumpFlags(args)
				if !errors.Is(err, errHelp) {
					t.Fatalf("err = %v, want errHelp", err)
				}
			})
			if strings.Contains(out, "required flag --dsn") {
				t.Fatalf("help should not require --dsn:\n%s", out)
			}
			if !strings.Contains(out, "--dsn") || !strings.Contains(out, "--output") {
				t.Fatalf("dump usage missing flags:\n%s", out)
			}
		})
	}
}
