//go:build !windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyExecutableTarget(t *testing.T, dir string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "dolly")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	return target
}

func captureUpdateStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), runErr
}

func TestRunUpdateUnixUpdatedE2E(t *testing.T) {
	target := copyExecutableTarget(t, t.TempDir())
	beforeSHA := fileSHA256(t, target)

	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	out, err := captureUpdateStdout(t, func() error {
		return runUpdateWithClient([]string{"--json"}, mock.client, updateTestConfig{
			installedVersion: "0.3.1",
			targetPath:       target,
		})
	})
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, out)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if result["status"] != "updated" {
		t.Fatalf("status = %v, want updated", result["status"])
	}
	if after := fileSHA256(t, target); after == beforeSHA {
		t.Fatal("target not updated")
	}
}

func TestRunUpdateUnixUpdatedTextE2E(t *testing.T) {
	target := copyExecutableTarget(t, t.TempDir())

	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	out, err := captureUpdateStdout(t, func() error {
		return runUpdateWithClient(nil, mock.client, updateTestConfig{
			installedVersion: "0.3.1",
			targetPath:       target,
		})
	})
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "updated dolly to v0.3.2") {
		t.Fatalf("stdout = %q", out)
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
