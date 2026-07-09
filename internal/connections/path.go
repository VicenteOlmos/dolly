package connections

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/VicenteOlmos/dolly/internal/config"
)

const (
	defaultProjectStore = ".dolly.connections.yaml"
	defaultXDGSubdir    = "dolly"
	defaultXDGFile      = "connections.yaml"
)

// ResolveStorePath returns the absolute path to the connections YAML store.
func ResolveStorePath(cfg *config.Config, cwd string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is nil")
	}
	if cfg.Connections.Path != "" {
		p := cfg.Connections.Path
		if filepath.IsAbs(p) {
			return p, nil
		}
		return filepath.Join(cwd, p), nil
	}
	scope := cfg.Connections.Scope
	if scope == "" {
		scope = "project"
	}
	switch scope {
	case "project":
		return filepath.Join(cwd, defaultProjectStore), nil
	case "xdg":
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve home dir: %w", err)
			}
			configHome = filepath.Join(home, ".config")
		}
		return filepath.Join(configHome, defaultXDGSubdir, defaultXDGFile), nil
	default:
		return "", fmt.Errorf("unknown connections.scope %q", scope)
	}
}
