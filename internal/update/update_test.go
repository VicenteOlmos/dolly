package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
