package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateHTTPSURL(t *testing.T) {
	if _, err := validateHTTPSURL("http://api.github.com/foo"); err == nil {
		t.Fatal("expected http rejection")
	}
	if _, err := validateHTTPSURL("https://evil.example/asset"); err == nil {
		t.Fatal("expected host rejection")
	}
	if _, err := validateHTTPSURL("https://api.github.com/repos/x/releases/latest"); err != nil {
		t.Fatalf("expected allowed host: %v", err)
	}
}

func TestDownloadRejectsOversize(t *testing.T) {
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Write(make([]byte, maxChecksumsBody+1))
		return rec.Result(), nil
	})

	_, err := downloadAsset(context.Background(), client, "https://release-assets.githubusercontent.com/mock/big", maxChecksumsBody)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestRedirectRejectsHTTP(t *testing.T) {
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/bad", http.StatusFound)
	}))
	defer next.Close()

	client := newHTTPClient()
	req, err := http.NewRequest(http.MethodGet, next.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "non-HTTPS") {
		t.Fatalf("err = %v", err)
	}
}

func releaseAssetGitHubURL(repo, tag, assetName string) string {
	return "https://github.com/" + repo + "/releases/download/" + tag + "/" + assetName
}

func TestDownloadFollowsCDNRedirectFromGitHubURL(t *testing.T) {
	repo := "VicenteOlmos/dolly"
	tag := "v0.3.2"
	asset := "dolly_linux_x86_64.tar.gz"
	want := []byte("archive-bytes")

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(want)
	}))
	defer cdn.Close()

	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL, http.StatusFound)
	}))
	defer github.Close()

	initial := releaseAssetGitHubURL(repo, tag, asset)
	client := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == initial {
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(github.URL, "http://")
			req.URL.Path = "/redirect"
			return http.DefaultClient.Do(req)
		}
		return http.DefaultClient.Do(req)
	})

	got, err := downloadAsset(context.Background(), client, initial, maxArchiveBody)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("body = %q, want %q", got, want)
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
