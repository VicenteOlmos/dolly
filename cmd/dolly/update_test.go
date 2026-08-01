package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/update"
)

func TestParseUpdateFlagsHelp(t *testing.T) {
	_, err := parseUpdateFlags([]string{"--help"})
	if !errors.Is(err, errHelp) {
		t.Fatalf("err = %v, want errHelp", err)
	}
}

func TestParseUpdateFlagsUnknown(t *testing.T) {
	_, err := parseUpdateFlags([]string{"--force"})
	if err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmitUpdateJSONOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		result *update.Result
		want   string
	}{
		{
			name: "available",
			result: &update.Result{
				OK: true, Command: "update", Status: update.StatusAvailable,
				InstalledVersion: "0.3.1", RemoteVersion: "v0.3.2", Asset: "dolly_linux_x86_64.tar.gz",
			},
			want: "available",
		},
		{
			name: "updated",
			result: &update.Result{
				OK: true, Command: "update", Status: update.StatusUpdated,
				InstalledVersion: "0.3.2", RemoteVersion: "v0.3.2",
			},
			want: "updated",
		},
		{
			name: "deferred",
			result: &update.Result{
				OK: true, Command: "update", Status: update.StatusDeferred,
				InstalledVersion: "0.3.1", RemoteVersion: "v0.3.2", Target: "C:\\dolly\\dolly.exe",
			},
			want: "deferred",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			if err := emitUpdateJSON(tc.result, nil); err != nil {
				t.Fatalf("emitUpdateJSON: %v", err)
			}
			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			var payload map[string]any
			if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
				t.Fatalf("json: %v\n%s", err, buf.String())
			}
			if payload["status"] != tc.want {
				t.Fatalf("status = %v, want %s", payload["status"], tc.want)
			}
		})
	}
}

func TestEmitUpdateTextOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		result *update.Result
		want   string
	}{
		{
			name: "deferred",
			result: &update.Result{
				OK: true, Status: update.StatusDeferred,
				Target: "/tmp/dolly", InstalledVersion: "0.3.1", RemoteVersion: "v0.3.2",
			},
			want: "update deferred:",
		},
		{
			name: "updated",
			result: &update.Result{
				OK: true, Status: update.StatusUpdated, RemoteVersion: "v0.3.2",
			},
			want: "updated dolly to v0.3.2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = w
			if err := emitUpdateText(tc.result, nil); err != nil {
				t.Fatalf("emitUpdateText: %v", err)
			}
			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			if !strings.Contains(buf.String(), tc.want) {
				t.Fatalf("stdout = %q, want %q", buf.String(), tc.want)
			}
		})
	}
}
