package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDispatchExitMatrix(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantStderr []string
		notStderr  []string
	}{
		{
			name:     "bare invocation",
			args:     []string{"dolly"},
			wantExit: 1,
			wantStderr: []string{
				"usage: dolly <command>",
				"dump",
				"restore",
				"config",
			},
		},
		{
			name:     "root short help",
			args:     []string{"dolly", "-h"},
			wantExit: 0,
			wantStderr: []string{
				"usage: dolly <command>",
				"dolly <command> --help",
			},
		},
		{
			name:       "root long help",
			args:       []string{"dolly", "--help"},
			wantExit:   0,
			wantStderr: []string{"usage: dolly <command>"},
		},
		{
			name:       "help subcommand rejected",
			args:       []string{"dolly", "help"},
			wantExit:   1,
			wantStderr: []string{"unknown command"},
			notStderr:  []string{"--dsn"},
		},
		{
			name:       "help with target rejected",
			args:       []string{"dolly", "help", "dump"},
			wantExit:   1,
			wantStderr: []string{"unknown command"},
			notStderr:  []string{"usage: dolly dump"},
		},
		{
			name:       "unknown subcommand",
			args:       []string{"dolly", "foo"},
			wantExit:   1,
			wantStderr: []string{"unknown command"},
			notStderr:  []string{"usage: dolly dump"},
		},
		{
			name:       "dump short help",
			args:       []string{"dolly", "dump", "-h"},
			wantExit:   0,
			wantStderr: []string{"usage: dolly dump", "--dsn"},
		},
		{
			name:       "dump help",
			args:       []string{"dolly", "dump", "--help"},
			wantExit:   0,
			wantStderr: []string{"--dsn", "--output"},
		},
		{
			name:       "restore help",
			args:       []string{"dolly", "restore", "-h"},
			wantExit:   0,
			wantStderr: []string{"usage: dolly restore", "--input"},
		},
		{
			name:       "clone help",
			args:       []string{"dolly", "clone", "--help"},
			wantExit:   0,
			wantStderr: []string{"usage: dolly clone", "-ff"},
		},
		{
			name:       "tui help without terminal",
			args:       []string{"dolly", "tui", "-h"},
			wantExit:   0,
			wantStderr: []string{"usage: dolly tui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := os.Stderr
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stderr = w

			origTerminal := isTerminal
			isTerminal = func(uintptr) bool { return false }
			t.Cleanup(func() {
				isTerminal = origTerminal
				os.Stderr = old
			})

			got := dispatch(tt.args)

			_ = w.Close()
			os.Stderr = old
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			stderr := buf.String()

			if got != tt.wantExit {
				t.Fatalf("dispatch exit = %d, want %d\nstderr:\n%s", got, tt.wantExit, stderr)
			}
			for _, sub := range tt.wantStderr {
				if !strings.Contains(stderr, sub) {
					t.Fatalf("stderr missing %q:\n%s", sub, stderr)
				}
			}
			for _, sub := range tt.notStderr {
				if strings.Contains(stderr, sub) {
					t.Fatalf("stderr should not contain %q:\n%s", sub, stderr)
				}
			}
		})
	}
}

func TestDispatchVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"subcommand", []string{"dolly", "version"}},
		{"root flag", []string{"dolly", "--version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := t.TempDir()
			t.Chdir(workDir)

			oldVersion, oldCommit, oldDate := version, commit, date
			version, commit, date = "1.2.3", "abc123", "2026-06-07"
			t.Cleanup(func() {
				version, commit, date = oldVersion, oldCommit, oldDate
			})

			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			t.Cleanup(func() { os.Stdout = oldStdout })

			got := dispatch(tt.args)

			_ = w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)

			if got != 0 {
				t.Fatalf("dispatch exit = %d, want 0", got)
			}
			if want := "dolly 1.2.3 (commit abc123, built 2026-06-07)\n"; buf.String() != want {
				t.Fatalf("stdout = %q, want %q", buf.String(), want)
			}
			if _, err := os.Stat("config.jsonc"); !os.IsNotExist(err) {
				t.Fatalf("version command should not bootstrap config.jsonc, stat err = %v", err)
			}
		})
	}
}

func TestRunVersionJSON(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "1.2.3", "abc123", "2026-06-07"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	err = runVersion([]string{"--json"})
	if err != nil {
		t.Fatalf("runVersion: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	stdout := buf.String()

	var result struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout)
	}
	if !result.OK {
		t.Fatal("expected ok=true")
	}
	if result.Command != "version" {
		t.Fatalf("command = %q, want version", result.Command)
	}
	if result.Version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", result.Version)
	}
	if result.Commit != "abc123" {
		t.Fatalf("commit = %q, want abc123", result.Commit)
	}
	if result.Date != "2026-06-07" {
		t.Fatalf("date = %q, want 2026-06-07", result.Date)
	}
}

func TestDispatchVersionJSON(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v2.0.0", "def456", "2026-07-01"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	got := dispatch([]string{"dolly", "version", "--json"})

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if got != 0 {
		t.Fatalf("dispatch exit = %d, want 0", got)
	}
	var result struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if !result.OK {
		t.Fatal("expected ok=true")
	}
	if result.Command != "version" {
		t.Fatalf("command = %q, want version", result.Command)
	}
	if result.Version != "v2.0.0" {
		t.Fatalf("version = %q, want v2.0.0", result.Version)
	}
}

func TestRunVersionHelp(t *testing.T) {
	// version --help prints usage, not version info.
	stderr := captureStderr(func() {
		exit := dispatch([]string{"dolly", "version", "--help"})
		if exit != 0 {
			t.Fatalf("dispatch exit = %d, want 0", exit)
		}
	})
	if !strings.Contains(stderr, "usage: dolly version") {
		t.Fatalf("stderr missing usage text, got: %s", stderr)
	}
	// Must not contain JSON output.
	if strings.Contains(stderr, `"ok"`) {
		t.Fatalf("stderr should not contain JSON, got: %s", stderr)
	}
}

func TestRunVersionJSONHelp(t *testing.T) {
	// version --json --help prints usage (help wins over --json).
	stderr := captureStderr(func() {
		exit := dispatch([]string{"dolly", "version", "--json", "--help"})
		if exit != 0 {
			t.Fatalf("dispatch exit = %d, want 0", exit)
		}
	})
	if !strings.Contains(stderr, "usage: dolly version") {
		t.Fatalf("stderr missing usage text, got: %s", stderr)
	}
	// Must not contain JSON output.
	if strings.Contains(stderr, `"ok"`) {
		t.Fatalf("stderr should not contain JSON when --help is present, got: %s", stderr)
	}
}
