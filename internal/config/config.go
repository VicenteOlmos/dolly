package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"
)

//go:embed config.jsonc.tmpl
var defaultConfigTemplate []byte

// Config defines the schema for config.jsonc.
type Config struct {
	Env struct {
		Path        string `json:"path"`
		URLVar      string `json:"url_var"`
		HostVar     string `json:"host_var"`
		PortVar     string `json:"port_var"`
		NameVar     string `json:"name_var"`
		UserVar     string `json:"user_var"`
		PasswordVar string `json:"password_var"`
	} `json:"env"`
	Clone struct {
		NameTemplate      string   `json:"name_template"`
		TargetURL         string   `json:"target_url"`
		TargetDir         string   `json:"target_dir"`
		DumpDir           string   `json:"dump_dir"`
		RestoreOnConflict string   `json:"restore_on_conflict"`
		Replace           bool     `json:"replace"`
		SkipCreate        bool     `json:"skip_create"`
		Strategy          string   `json:"strategy"`
		Schemas           []string `json:"schemas"`
		Preflight         struct {
			CachePermissions     bool   `json:"cache_permissions"`
			CachePermissionsPath string `json:"cache_permissions_path"`
			CachePermissionsTTL  string `json:"cache_permissions_ttl"`
		} `json:"preflight"`
	} `json:"clone"`
	Sanitization struct {
		Enabled bool `json:"enabled"`
	} `json:"sanitization"`
	Subset struct {
		Percent          int `json:"percent"`
		SeedFile         string `json:"seed_file"`
		MaxRowsPerTable  int `json:"max_rows_per_table"`
	} `json:"subset"`
	Dump struct {
		OutputDir         string   `json:"output_dir"`
		SlowChunkSize     int      `json:"slow_chunk_size"`
		SlowRetryMax      int      `json:"slow_retry_max"`
		SlowRetryBase     string   `json:"slow_retry_base"`
		IncludeTables     []string `json:"include_tables"`
		ExcludeTables     []string `json:"exclude_tables"`
		IncludeTableFiles []string `json:"include_table_files"`
		ExcludeTableFiles []string `json:"exclude_table_files"`
		ChunkTables       []string `json:"chunk_tables"`
		ChunkTableFiles   []string `json:"chunk_table_files"`
	} `json:"dump"`
	SaveConnections bool `json:"save_connections"`
	DB              struct {
		// MaxOpenConns limits the sql.DB pool size (default 5).
		MaxOpenConns int `json:"max_open_conns"`
		// StatementTimeout aborts queries that exceed this duration (Go duration string, default "5min").
		// Applied as the PostgreSQL statement_timeout session parameter on every connection.
		// Set to empty string to disable.
		StatementTimeout string `json:"statement_timeout"`
	} `json:"db"`
	TUI struct {
		// SectionEntry controls whether section screens open in overview ("overview")
		// or drilled into the first section ("inside").
		SectionEntry string `json:"section_entry"`
		// Theme names a built-in TUI palette (catppuccin-mocha, rose-pine, dracula, …).
		Theme string `json:"theme"`
	} `json:"tui"`
	Connections struct {
		Scope   string `json:"scope"`
		Path    string `json:"path"`
		Encrypt bool   `json:"encrypt"`
		// Default names the saved profile pre-selected in the TUI connect screen.
		Default string `json:"default"`
	} `json:"connections"`
}

// DefaultTemplate returns the embedded config.jsonc template bytes.
func DefaultTemplate() []byte {
	cp := make([]byte, len(defaultConfigTemplate))
	copy(cp, defaultConfigTemplate)
	return cp
}

// DefaultConfig returns a Config populated with hard-coded defaults.
func DefaultConfig() *Config {
	cfg := &Config{}
	cfg.Env.Path = ".env"
	cfg.Env.URLVar = "DB_URL"
	cfg.Env.HostVar = "DB_HOST"
	cfg.Env.PortVar = "DB_PORT"
	cfg.Env.NameVar = "DB_NAME"
	cfg.Env.UserVar = "DB_USER"
	cfg.Env.PasswordVar = "DB_PASSWORD"
	cfg.Clone.NameTemplate = "{db}_dolly_{n}"
	cfg.Clone.RestoreOnConflict = "error"
	cfg.Clone.Strategy = "schema-replay"
	cfg.Clone.Preflight.CachePermissionsPath = ".dolly/permissions-cache.yaml"
	cfg.Clone.Preflight.CachePermissionsTTL = "24h"
	cfg.Connections.Scope = "project"
	cfg.Dump.OutputDir = "dolly_dump"
	cfg.Dump.SlowChunkSize = 1000
	cfg.Dump.SlowRetryBase = "500ms"
	cfg.Dump.IncludeTables = []string{}
	cfg.Dump.ExcludeTables = []string{}
	cfg.Dump.IncludeTableFiles = []string{}
	cfg.Dump.ExcludeTableFiles = []string{}
	cfg.Dump.ChunkTables = []string{}
	cfg.Dump.ChunkTableFiles = []string{}
	cfg.DB.MaxOpenConns = 5
	cfg.DB.StatementTimeout = "5min"
	cfg.TUI.SectionEntry = "inside"
	cfg.TUI.Theme = "catppuccin-mocha"
	return cfg
}

// ClonePermissionCacheTTL parses clone.preflight.cache_permissions_ttl (default 24h).
func (c *Config) ClonePermissionCacheTTL() (time.Duration, error) {
	raw := c.Clone.Preflight.CachePermissionsTTL
	if raw == "" {
		return 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse cache_permissions_ttl %q: %w", raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("cache_permissions_ttl must be positive, got %q", raw)
	}
	return d, nil
}

// ResolveConfigPath returns the application config file path ("config.jsonc").
func ResolveConfigPath() string {
	return "config.jsonc"
}

// BootstrapConfig writes the embedded default template to path if the file
// does not already exist. It is a no-op when the file is present.
// The caller should treat errors as non-fatal; defaults still apply.
func BootstrapConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	if err := os.WriteFile(path, defaultConfigTemplate, 0o600); err != nil {
		return fmt.Errorf("bootstrap config: %w", err)
	}
	return nil
}

// SaveConfig writes cfg to path with permission 0600. For JSONC files it patches
// the existing document in place so comments and formatting are preserved; only
// changed values are updated. Unparseable JSONC falls back to clean indented JSON.
// If marshalling fails the file is not touched.
func SaveConfig(cfg *Config, path string) error {
	if err := rejectYAMLConfigPath(path); err != nil {
		return err
	}
	return saveConfigJSONC(cfg, path)
}

// LoadConfig reads the config file at path and overlays it onto DefaultConfig.
// Only JSONC (or JSON) paths are supported; .yaml/.yml paths return an error.
// If the file does not exist, DefaultConfig is returned without error.
// Malformed content returns an error.
func LoadConfig(path string) (*Config, error) {
	if err := rejectYAMLConfigPath(path); err != nil {
		return nil, err
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	plain := stripJSONC(data)
	if err := json.Unmarshal(plain, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func rejectYAMLConfigPath(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		return fmt.Errorf("%s: YAML config is not supported; use config.jsonc", path)
	}
	return nil
}
