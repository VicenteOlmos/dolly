package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/update"
)

type mockUpdateHTTP struct {
	client update.HTTPDoer
}

func (m *mockUpdateHTTP) install(t *testing.T, tag string) {
	t.Helper()
	assetName, err := update.CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	archive := buildTestArchive(t, "dolly", []byte("new-binary"))
	checksums := buildTestChecksum(t, assetName, archive)

	m.client = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptestRecorder()
		switch {
		case strings.Contains(req.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(rec).Encode(map[string]any{
				"tag_name":   tag,
				"draft":      false,
				"prerelease": false,
				"assets": []map[string]any{
					{"name": assetName, "browser_download_url": "https://github.com/VicenteOlmos/dolly/releases/download/" + tag + "/" + assetName},
					{"name": "checksums.txt", "browser_download_url": "https://github.com/VicenteOlmos/dolly/releases/download/" + tag + "/checksums.txt"},
				},
			})
		case strings.HasSuffix(req.URL.Path, "/"+assetName):
			rec.Write(archive)
		case strings.HasSuffix(req.URL.Path, "/checksums.txt"):
			rec.Write(checksums)
		default:
			rec.WriteHeader(http.StatusNotFound)
		}
		return rec.Result(), nil
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type testResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func httptestRecorder() *testResponseRecorder {
	return &testResponseRecorder{header: make(http.Header), code: http.StatusOK}
}

func (r *testResponseRecorder) Header() http.Header         { return r.header }
func (r *testResponseRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *testResponseRecorder) WriteHeader(code int)        { r.code = code }
func (r *testResponseRecorder) Result() *http.Response {
	return &http.Response{
		StatusCode: r.code,
		Header:     r.header,
		Body:       io.NopCloser(bytes.NewReader(r.body.Bytes())),
	}
}

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

func TestRunUpdateJSONCurrent(t *testing.T) {
	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	err = runUpdateWithClient([]string{"--json"}, mock.client, updateTestConfig{installedVersion: "0.3.2"})
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, buf.String())
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if result["status"] != "current" {
		t.Fatalf("status = %v", result["status"])
	}
}

func TestRunUpdateJSONAvailable(t *testing.T) {
	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	err = runUpdateWithClient([]string{"--check", "--json"}, mock.client, updateTestConfig{installedVersion: "0.3.1"})
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, buf.String())
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if result["status"] != "available" {
		t.Fatalf("status = %v", result["status"])
	}
}

func TestRunUpdateTextAvailable(t *testing.T) {
	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	err = runUpdateWithClient([]string{"--check"}, mock.client, updateTestConfig{installedVersion: "0.3.1"})
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "update available:") {
		t.Fatalf("stdout = %s", buf.String())
	}
}

func TestRunUpdateTextCurrent(t *testing.T) {
	mock := mockUpdateHTTP{}
	mock.install(t, "v0.3.2")

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	err = runUpdateWithClient(nil, mock.client, updateTestConfig{installedVersion: "0.3.2"})
	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("runUpdate: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "up to date") {
		t.Fatalf("stdout = %s", buf.String())
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

func TestRunUpdateDevJSONFailure(t *testing.T) {
	oldVersion := version
	version = "dev"
	t.Cleanup(func() { version = oldVersion })

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	err = runUpdate([]string{"--json"})
	w.Close()
	os.Stderr = oldStderr

	if !errors.Is(err, errJSONHandled) {
		t.Fatalf("err = %v, want errJSONHandled", err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"status": "failed"`) {
		t.Fatalf("stderr = %s", buf.String())
	}
}

type updateTestConfig struct {
	installedVersion string
	targetPath       string
}

func runUpdateWithClient(args []string, client update.HTTPDoer, cfg updateTestConfig) error {
	flags, err := parseUpdateFlags(args)
	if errors.Is(err, errHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	installedVersion := cfg.installedVersion
	if installedVersion == "" {
		installedVersion = version
	}
	opts := update.Options{
		HTTP:             client,
		InstalledVersion: installedVersion,
		CheckOnly:        flags.check,
		TargetPath:       cfg.targetPath,
	}
	result, runErr := update.Run(context.Background(), opts)
	if runErr != nil && result == nil {
		result = &update.Result{OK: false, Command: "update", Status: update.StatusFailed, Error: runErr.Error()}
	}
	if flags.json {
		return emitUpdateJSON(result, runErr)
	}
	return emitUpdateText(result, runErr)
}
