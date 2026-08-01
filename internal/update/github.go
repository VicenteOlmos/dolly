package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	defaultRepo      = "VicenteOlmos/dolly"
	maxRedirects     = 5
	overallTimeout   = 2 * time.Minute
	requestTimeout   = 60 * time.Second
	dialTimeout      = 10 * time.Second
	maxAPIResponse   = 1 << 20  // 1 MiB
	maxChecksumsBody = 64 << 10 // 64 KiB
	maxArchiveBody   = 64 << 20 // 64 MiB
)

// HTTPDoer performs HTTP requests. *http.Client satisfies this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type releaseAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type releaseMetadata struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type selectedRelease struct {
	Version        Version
	Asset          releaseAsset
	ChecksumsAsset releaseAsset
}

func latestReleaseURL(repo string) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
}

func allowedHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, ":443"))
	switch host {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com":
		return true
	default:
		return false
	}
}

func validateHTTPSURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("URL must use HTTPS")
	}
	if !allowedHost(u.Hostname()) {
		return nil, fmt.Errorf("URL host %q is not allowed", u.Hostname())
	}
	return u, nil
}

func newHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: dialTimeout,
		}).DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: dialTimeout,
	}
	return &http.Client{
		Timeout:   requestTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTPS URL")
			}
			if !allowedHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to disallowed host %q", req.URL.Hostname())
			}
			req.Header.Del("Authorization")
			return nil
		},
	}
}

func fetchLatestRelease(ctx context.Context, doer HTTPDoer, repo, assetName string) (selectedRelease, error) {
	if doer == nil {
		doer = newHTTPClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL(repo), nil)
	if err != nil {
		return selectedRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "dolly-update")

	resp, err := doer.Do(req)
	if err != nil {
		return selectedRelease{}, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, maxAPIResponse)
	if err != nil {
		return selectedRelease{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return selectedRelease{}, fmt.Errorf("latest release API returned %s", resp.Status)
	}

	var meta releaseMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return selectedRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	if meta.Draft {
		return selectedRelease{}, fmt.Errorf("latest release is a draft")
	}
	if meta.Prerelease {
		return selectedRelease{}, fmt.Errorf("latest release is a prerelease")
	}
	if strings.TrimSpace(meta.TagName) == "" {
		return selectedRelease{}, fmt.Errorf("latest release is missing tag_name")
	}

	remote, err := ParseVersion(meta.TagName)
	if err != nil {
		return selectedRelease{}, fmt.Errorf("latest release tag: %w", err)
	}

	var selected *releaseAsset
	for i := range meta.Assets {
		if meta.Assets[i].Name != assetName {
			continue
		}
		if selected != nil {
			return selectedRelease{}, fmt.Errorf("release %s has duplicate asset %q", meta.TagName, assetName)
		}
		selected = &meta.Assets[i]
	}
	if selected == nil {
		return selectedRelease{}, fmt.Errorf("release %s has no asset %q", meta.TagName, assetName)
	}
	if selected.BrowserDownloadURL == "" {
		return selectedRelease{}, fmt.Errorf("asset %q is missing download URL", assetName)
	}
	if _, err := validateHTTPSURL(selected.BrowserDownloadURL); err != nil {
		return selectedRelease{}, fmt.Errorf("asset %q download URL: %w", assetName, err)
	}
	if err := validateReleaseAssetURL(repo, meta.TagName, assetName, selected.BrowserDownloadURL); err != nil {
		return selectedRelease{}, fmt.Errorf("asset %q download URL: %w", assetName, err)
	}

	checksums, err := findChecksumAsset(repo, meta.TagName, meta.Assets)
	if err != nil {
		return selectedRelease{}, err
	}

	return selectedRelease{
		Version:        remote,
		Asset:          *selected,
		ChecksumsAsset: checksums,
	}, nil
}

func downloadAsset(ctx context.Context, doer HTTPDoer, assetURL string, limit int64) ([]byte, error) {
	if doer == nil {
		doer = newHTTPClient()
	}
	if _, err := validateHTTPSURL(assetURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dolly-update")

	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download asset: %w", err)
	}
	defer resp.Body.Close()

	body, err := readLimited(resp.Body, limit)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}
	return body, nil
}

func findChecksumAsset(repo, tag string, assets []releaseAsset) (releaseAsset, error) {
	var found *releaseAsset
	for i := range assets {
		if assets[i].Name != "checksums.txt" {
			continue
		}
		if found != nil {
			return releaseAsset{}, fmt.Errorf("release has duplicate checksums.txt asset")
		}
		found = &assets[i]
	}
	if found == nil {
		return releaseAsset{}, fmt.Errorf("release is missing checksums.txt")
	}
	if found.BrowserDownloadURL == "" {
		return releaseAsset{}, fmt.Errorf("checksums.txt is missing download URL")
	}
	if _, err := validateHTTPSURL(found.BrowserDownloadURL); err != nil {
		return releaseAsset{}, fmt.Errorf("checksums.txt download URL: %w", err)
	}
	if err := validateReleaseAssetURL(repo, tag, "checksums.txt", found.BrowserDownloadURL); err != nil {
		return releaseAsset{}, fmt.Errorf("checksums.txt download URL: %w", err)
	}
	return *found, nil
}

func validateReleaseAssetURL(repo, tag, assetName, rawURL string) error {
	if strings.Contains(strings.ToLower(rawURL), "%2f") ||
		strings.Contains(strings.ToLower(rawURL), "%5c") ||
		strings.Contains(strings.ToLower(rawURL), "%2e") {
		return fmt.Errorf("encoded path segments are not allowed")
	}

	u, err := validateHTTPSURL(rawURL)
	if err != nil {
		return err
	}
	if u.Fragment != "" {
		return fmt.Errorf("URL fragment is not allowed")
	}

	if u.User != nil {
		return fmt.Errorf("URL userinfo is not allowed")
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" {
		return fmt.Errorf("release asset URL must use github.com")
	}
	if u.Port() != "" {
		return fmt.Errorf("URL port is not allowed")
	}
	wantPath := path.Join("/", repo, "releases", "download", tag, assetName)
	gotPath := path.Clean(u.EscapedPath())
	if gotPath != wantPath {
		return fmt.Errorf("URL path does not match release asset")
	}
	if u.RawQuery != "" {
		return fmt.Errorf("URL query is not allowed")
	}
	return nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d byte limit", limit)
	}
	return data, nil
}
