package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseChecksumsMissingEntry(t *testing.T) {
	asset := "dolly_linux_x86_64.tar.gz"
	other := strings.Repeat("a", 64) + "  other.txt"
	_, err := parseChecksums([]byte(other), asset)
	if err == nil || !strings.Contains(err.Error(), "no entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyArchiveSHA256Mismatch(t *testing.T) {
	err := verifyArchiveSHA256([]byte("data"), strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsChecksumMismatchWithoutMutation(t *testing.T) {
	assetName, err := CurrentAsset()
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("#!/bin/sh\necho newer\n")
	archive := buildCurrentArchive(t, content)
	wrongChecksums := []byte(checksumLine(assetName, []byte("wrong-archive")))

	target := writeFakeBinary(t, t.TempDir(), "dolly", 0o755)
	before := fileSHA256(t, target)

	result, err := Run(context.Background(), Options{
		HTTP:             mockReleaseClient(t, assetName, archive, wrongChecksums, "v0.3.2"),
		InstalledVersion: "0.3.1",
		TargetPath:       target,
	})
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("err = %v", err)
	}
	if result == nil || result.Status != StatusFailed {
		t.Fatalf("result = %+v", result)
	}
	if after := fileSHA256(t, target); after != before {
		t.Fatal("target mutated on checksum mismatch")
	}
}

func TestFetchLatestReleaseRejectsMalformedTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName: "v1.2.3+",
			Assets:  []releaseAsset{{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v1.2.3+", "dolly_linux_x86_64.tar.gz")}},
		})
	}))
	defer srv.Close()

	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	})

	_, err := fetchLatestRelease(context.Background(), client, "VicenteOlmos/dolly", "dolly_linux_x86_64.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadRejectsArchiveOversize(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Write(make([]byte, maxArchiveBody+1))
		return rec.Result(), nil
	})

	_, err := downloadAsset(context.Background(), client, "https://release-assets.githubusercontent.com/mock/big", maxArchiveBody)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadRejectsAPIOversize(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Write(make([]byte, maxAPIResponse+1))
		return rec.Result(), nil
	})

	_, err := downloadAsset(context.Background(), client, "https://api.github.com/repos/x/releases/latest", maxAPIResponse)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestRedirectRejectsDisallowedHost(t *testing.T) {
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/bad", http.StatusFound)
	}))
	defer next.Close()

	client := newHTTPClient()
	req, err := http.NewRequest(http.MethodGet, next.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "disallowed host") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadContextTimeout(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := downloadAsset(ctx, client, "https://release-assets.githubusercontent.com/mock/slow", maxChecksumsBody)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetchLatestReleaseRejectsDuplicateAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName: "v0.3.2",
			Assets: []releaseAsset{
				{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v0.3.2", "dolly_linux_x86_64.tar.gz")},
				{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v0.3.2", "dolly_linux_x86_64.tar.gz")},
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
	if err == nil || !strings.Contains(err.Error(), "duplicate asset") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchLatestReleaseRejectsDuplicateChecksums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releaseMetadata{
			TagName: "v0.3.2",
			Assets: []releaseAsset{
				{Name: "dolly_linux_x86_64.tar.gz", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v0.3.2", "dolly_linux_x86_64.tar.gz")},
				{Name: "checksums.txt", BrowserDownloadURL: releaseAssetGitHubURL("VicenteOlmos/dolly", "v0.3.2", "checksums.txt")},
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
	if err == nil || !strings.Contains(err.Error(), "duplicate checksums.txt") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseVersionRejectsMalformedBuildMetadata(t *testing.T) {
	cases := []string{"v1.2.3+", "v1.2.3+bad!", "v1.2.3+abc."}
	for _, raw := range cases {
		_, err := ParseVersion(raw)
		if err == nil {
			t.Fatalf("ParseVersion(%q) accepted malformed build metadata", raw)
		}
	}
}

func TestExtractRejectsAbsoluteArchiveMember(t *testing.T) {
	archive := buildTarGz(t, "/dolly", []byte("bad"))
	_, _, err := extractAndStage(archive, "dolly_linux_x86_64.tar.gz", "linux", t.TempDir())
	if err == nil {
		t.Fatal("absolute archive member was accepted")
	}
}
