package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchLatestReleaseRejectsPrerelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName:    "v0.3.2",
			Prerelease: true,
			Assets:     []releaseAsset{{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: "https://github.com/x"}},
		})
	}))
	defer srv.Close()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	})

	_, err := fetchLatestRelease(context.Background(), client, "VicenteOlmos/dolly", "dolly_linux_x86_64.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "prerelease") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateReleaseAssetURL(t *testing.T) {
	repo := "VicenteOlmos/dolly"
	tag := "v0.3.2"
	asset := "dolly_linux_x86_64.tar.gz"

	valid := []string{
		"https://github.com/VicenteOlmos/dolly/releases/download/v0.3.2/dolly_linux_x86_64.tar.gz",
	}
	for _, raw := range valid {
		if err := validateReleaseAssetURL(repo, tag, asset, raw); err != nil {
			t.Fatalf("validateReleaseAssetURL(%q) = %v", raw, err)
		}
	}

	invalid := []struct {
		raw string
	}{
		{"https://github.com/Other/repo/releases/download/v0.3.2/dolly_linux_x86_64.tar.gz"},
		{"https://github.com/VicenteOlmos/dolly/releases/download/v0.3.1/dolly_linux_x86_64.tar.gz"},
		{"https://github.com/VicenteOlmos/dolly/releases/download/v0.3.2/other.tar.gz"},
		{releaseAssetCDNURL(asset)},
		{"https://release-assets.githubusercontent.com/mock/checksums.txt"},
		{"https://release-assets.githubusercontent.com/mock/%2e%2e/dolly_linux_x86_64.tar.gz"},
		{"https://github.com/VicenteOlmos/dolly/releases/download/v0.3.2/dolly_linux_x86_64.tar.gz?token=1"},
	}
	for _, tc := range invalid {
		if err := validateReleaseAssetURL(repo, tag, asset, tc.raw); err == nil {
			t.Fatalf("validateReleaseAssetURL(%q) accepted unsafe URL", tc.raw)
		}
	}
}

func TestFetchLatestReleaseRejectsMismatchedAssetURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName: "v0.3.2",
			Assets: []releaseAsset{
				{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: "https://github.com/Other/repo/releases/download/v0.3.2/dolly_linux_x86_64.tar.gz"},
				{Name: "checksums.txt", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v0.3.2", "checksums.txt")},
			},
		})
	}))
	defer srv.Close()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	})

	_, err := fetchLatestRelease(context.Background(), client, "VicenteOlmos/dolly", "dolly_linux_x86_64.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "download URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchLatestReleaseRejectsDirectCDNMetadataURL(t *testing.T) {
	repo := "VicenteOlmos/dolly"
	tag := "v0.3.2"
	asset := "dolly_linux_x86_64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName: tag,
			Assets: []releaseAsset{
				{Name: asset, BrowserDownloadURL: releaseAssetCDNURL(asset)},
				{Name: "checksums.txt", BrowserDownloadURL: releaseAssetCDNURL("checksums.txt")},
			},
		})
	}))
	defer srv.Close()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	})

	_, err := fetchLatestRelease(context.Background(), client, repo, asset)
	if err == nil || !strings.Contains(err.Error(), "download URL") {
		t.Fatalf("err = %v", err)
	}
}

func releaseAssetCDNURL(assetName string) string {
	return "https://release-assets.githubusercontent.com/mock/" + assetName
}

func mockReleaseClient(t *testing.T, assetName string, archive, checksums []byte, tag string) HTTPDoer {
	t.Helper()
	repo := defaultRepo
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		switch {
		case strings.Contains(req.URL.Path, "/releases/latest"):
			_ = json.NewEncoder(rec).Encode(releaseMetadata{
				TagName: tag,
				Assets: []releaseAsset{
					{Name: assetName, BrowserDownloadURL: releaseAssetGitHubURL(repo, tag, assetName)},
					{Name: "checksums.txt", BrowserDownloadURL: releaseAssetGitHubURL(repo, tag, "checksums.txt")},
				},
			})
		case strings.Contains(req.URL.Path, "/releases/download/"):
			if strings.HasSuffix(req.URL.Path, "/"+assetName) {
				rec.Write(archive)
			} else if strings.HasSuffix(req.URL.Path, "/checksums.txt") {
				rec.Write(checksums)
			} else {
				rec.WriteHeader(http.StatusNotFound)
			}
		default:
			rec.WriteHeader(http.StatusNotFound)
		}
		return rec.Result(), nil
	})
}

func TestRunCurrentNoMutation(t *testing.T) {
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("binary")
	archive := buildCurrentArchive(t, content)
	checksums := []byte(checksumLine(assetName, archive))

	target := writeFakeBinary(t, t.TempDir(), "dolly", 0o755)
	before := fileSHA256(t, target)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.1"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
		CheckOnly:        true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusCurrent {
		t.Fatalf("status = %s, want current", result.Status)
	}
	if after := fileSHA256(t, target); after != before {
		t.Fatal("target mutated on current")
	}
}

func TestRunAvailableCheckVerifiesWithoutMutation(t *testing.T) {
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho newer\n")
	archive := buildCurrentArchive(t, content)
	checksums := []byte(checksumLine(assetName, archive))

	target := writeFakeBinary(t, t.TempDir(), "dolly", 0o755)
	before := fileSHA256(t, target)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, checksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
		CheckOnly:        true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != StatusAvailable {
		t.Fatalf("status = %s, want available", result.Status)
	}
	if after := fileSHA256(t, target); after != before {
		t.Fatal("target mutated on check")
	}
}

func TestRunDevBuildRejected(t *testing.T) {
	result, err := Run(context.Background(), Options{InstalledVersion: "dev"})
	if err == nil {
		t.Fatal("expected dev rejection")
	}
	if result == nil || result.Status != StatusFailed {
		t.Fatalf("result = %+v", result)
	}
}

func writeFakeBinary(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("old-binary"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
