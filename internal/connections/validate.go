package connections

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/config"
)

// ValidateConnectionsConfig checks connections.scope and connections.path before
// persisting config from the TUI.
func ValidateConnectionsConfig(cfg *config.Config, cwd string) error {
	if cfg == nil {
		return nil
	}
	scope := strings.TrimSpace(cfg.Connections.Scope)
	cfg.Connections.Scope = scope
	if scope != "" && scope != "project" && scope != "xdg" {
		return fmt.Errorf("unknown connections.scope %q (supported: project, xdg)", scope)
	}
	path := strings.TrimSpace(cfg.Connections.Path)
	cfg.Connections.Path = path
	if path == "" {
		return nil
	}
	abs := path
	if !filepath.IsAbs(abs) {
		if cwd == "" {
			cwd = "."
		}
		abs = filepath.Join(cwd, path)
	}
	parent := filepath.Dir(abs)
	if err := validateConnectionsParentDir(parent); err != nil {
		return err
	}
	return nil
}

func validateConnectionsParentDir(parent string) error {
	if parent == "" || parent == "." {
		return nil
	}
	info, err := os.Stat(parent)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("connections.path: parent %q is not a directory", parent)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("connections.path: parent %q: %w", parent, err)
	}
	var missing []string
	cur := parent
	for {
		st, statErr := os.Stat(cur)
		if statErr == nil {
			if !st.IsDir() {
				return fmt.Errorf("connections.path: parent %q is not a directory", cur)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("connections.path: parent %q: %w", cur, statErr)
		}
		missing = append(missing, cur)
		parentOf := filepath.Dir(cur)
		if parentOf == cur {
			return fmt.Errorf("connections.path: cannot create parent directory %q", parent)
		}
		cur = parentOf
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("connections.path: cannot create parent directory %q: %w", parent, err)
	}
	for _, p := range missing {
		_ = os.Remove(p)
	}
	return nil
}

// ValidateDSNTLSFiles ensures sslrootcert, sslcert, and sslkey paths in dsn are readable files.
// Missing or unreadable files fail before opening a database connection.
func ValidateDSNTLSFiles(dsn string) error {
	params, err := dsnTLSFileParams(dsn)
	if err != nil {
		return nil
	}
	for _, key := range []string{"sslrootcert", "sslcert", "sslkey"} {
		path := strings.TrimSpace(params.Get(key))
		if path == "" {
			continue
		}
		if key == "sslrootcert" && path == "system" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("%s %q: %w", key, path, err)
		}
		_ = f.Close()
	}
	return nil
}

func dsnTLSFileParams(dsn string) (url.Values, error) {
	if strings.Contains(dsn, "://") {
		clean, _, err := SubprocessDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("parse DSN: %w", err)
		}
		u, err := parsePostgresURL(clean)
		if err != nil {
			return nil, fmt.Errorf("parse DSN: %w", err)
		}
		return u.Query(), nil
	}
	if strings.Contains(dsn, "=") && !strings.Contains(dsn, "://") {
		tokens, err := tokenizeKeywordDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("parse DSN: %w", err)
		}
		q := make(url.Values)
		for _, tok := range tokens {
			key := strings.ToLower(tok.key)
			if key == "sslrootcert" || key == "sslcert" || key == "sslkey" {
				q.Set(key, libpqValueBytes(tok.rawValue))
			}
		}
		return q, nil
	}
	return url.Values{}, nil
}
