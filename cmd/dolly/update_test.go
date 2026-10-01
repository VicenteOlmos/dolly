package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestDispatchUpdateInternalHidden(t *testing.T) {
	handled, code := dispatchUpdateInternal([]string{"dolly", "__update-helper"})
	if !handled || code == 0 {
		t.Fatalf("handled=%v code=%d, want handled failure", handled, code)
	}
}

func TestDispatchUpdateHelpDoesNotExposeHelper(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	exit := dispatch([]string{"dolly", "update", "--help"})
	w.Close()
	os.Stderr = oldStderr

	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, r)
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if strings.Contains(stderr.String(), "__update-helper") {
		t.Fatal("helper mode leaked into help")
	}
}

func TestDispatchUpdateBeforeConfigBootstrap(t *testing.T) {
	workDir := t.TempDir()
	t.Chdir(workDir)

	oldVersion := version
	version = "0.3.2"
	t.Cleanup(func() { version = oldVersion })

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	oldStderr := os.Stderr
	sr, sw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = sw

	exit := dispatch([]string{"dolly", "update", "--help"})
	sw.Close()
	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var stdout, stderr bytes.Buffer
	_, _ = io.Copy(&stdout, r)
	_, _ = io.Copy(&stderr, sr)

	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: dolly update") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if _, err := os.Stat("config.jsonc"); !os.IsNotExist(err) {
		t.Fatalf("update help should not bootstrap config, stat err=%v", err)
	}
}

type updateTestConfig struct {
	installedVersion string
	targetPath       string
}

func runUpdateWithClient(args []string, client update.HTTPDoer, cfg updateTestConfig) error {
	return runUpdateWithContext(context.Background(), args, &updateRunInject{
		http:             client,
		installedVersion: cfg.installedVersion,
		targetPath:       cfg.targetPath,
	})
}

func TestRunUpdateContextCancel(t *testing.T) {
	assetName, err := update.CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	tag := "v0.3.2"

	downloadStarted := make(chan struct{})
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
		case strings.HasSuffix(req.URL.Path, "/checksums.txt"):
			select {
			case downloadStarted <- struct{}{}:
			default:
			}
			<-req.Context().Done()
			return nil, req.Context().Err()
		case strings.HasSuffix(req.URL.Path, "/"+assetName):
			<-req.Context().Done()
			return nil, req.Context().Err()
		default:
			rec.WriteHeader(http.StatusNotFound)
		}
		return rec.Result(), nil
	})

	target := filepath.Join(t.TempDir(), "dolly")
	if err := os.WriteFile(target, []byte("dolly"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldStderr := os.Stderr
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = stderrW
	stderrText := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, stderrR)
		stderrText <- buf.String()
	}()
	finishStderr := func() string {
		os.Stderr = oldStderr
		_ = stderrW.Close()
		return <-stderrText
	}

	var runErr error
	done := make(chan struct{})
	go func() {
		runErr = runUpdateWithContext(ctx, []string{"--check"}, &updateRunInject{
			http:             client,
			installedVersion: "0.3.1",
			targetPath:       target,
		})
		close(done)
	}()

	select {
	case <-downloadStarted:
		cancel()
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatalf("download did not start; err=%v stderr=%s", runErr, finishStderr())
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("runUpdate did not return promptly after cancel; stderr=%s", finishStderr())
	}

	stderr := finishStderr()

	if !errors.Is(runErr, errTextHandled) {
		t.Fatalf("err = %v, want errTextHandled", runErr)
	}
	if !strings.Contains(stderr, "context canceled") {
		t.Fatalf("stderr = %q, want context canceled", stderr)
	}
}

func TestDispatchUpdateTextFailureSingleStderrLine(t *testing.T) {
	oldVersion := version
	version = "dev"
	t.Cleanup(func() { version = oldVersion })

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	exit := dispatch([]string{"dolly", "update"})
	w.Close()
	os.Stderr = oldStderr

	if exit == 0 {
		t.Fatal("expected failure exit code")
	}
	var stderr bytes.Buffer
	_, _ = io.Copy(&stderr, r)
	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr line count = %d, want 1; stderr=%q", len(lines), stderr.String())
	}
	if !strings.Contains(lines[0], "development build") {
		t.Fatalf("stderr = %q", lines[0])
	}
}
