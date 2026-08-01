package update

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
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
