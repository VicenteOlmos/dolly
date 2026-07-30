package clone

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// PermissionCacheConfig controls on-disk caching of successful permission preflight.
type PermissionCacheConfig struct {
	Enabled bool
	Path    string
	TTL     time.Duration
}

// permissionCacheEntry is one cached successful permission check.
type permissionCacheEntry struct {
	Key        string    `yaml:"key"`
	Strategy   string    `yaml:"strategy"`
	SkipCreate bool      `yaml:"skip_create"`
	CloneName  string    `yaml:"clone_name"`
	SourceDB   string    `yaml:"source_db"`
	SourceUser string    `yaml:"source_user"`
	SourceHost string    `yaml:"source_host"`
	SourcePort string    `yaml:"source_port"`
	TargetHost string    `yaml:"target_host,omitempty"`
	TargetPort string    `yaml:"target_port,omitempty"`
	TargetDB   string    `yaml:"target_db,omitempty"`
	SameInst   bool      `yaml:"same_instance"`
	Role       string    `yaml:"role"`
	CheckedAt  time.Time `yaml:"checked_at"`
	ExpiresAt  time.Time `yaml:"expires_at"`
}

type permissionCacheDoc struct {
	Entries []permissionCacheEntry `yaml:"entries"`
}

var (
	loadPermissionCacheFile    = loadPermissionCacheFromPath
	savePermissionCacheFile    = savePermissionCacheToPath
	replacePermissionCacheFile = atomicReplace
	permissionCacheNow         = time.Now
)

func permissionCacheKey(dsns preflightDSNs, opts Options, strategy string) (string, error) {
	src, err := parseDSNIdentity(dsns.sourceDSN)
	if err != nil {
		return "", err
	}
	tgt, err := parseDSNIdentity(dsns.targetDSN)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf(
		"check_version=5\nstrategy=%s\nskip_create=%t\nclone=%s\nsource=%s:%s:%s:%s\ntarget=%s:%s:%s\nsame=%t",
		strategy,
		opts.SkipCreate,
		opts.CloneName,
		src.host, src.port, src.db, src.user,
		tgt.host, tgt.port, tgt.db,
		dsns.sameInst,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

type dsnIdentity struct {
	host, port, db, user string
}

func parseDSNIdentity(dsn string) (dsnIdentity, error) {
	u, err := parsePostgresURL(dsn)
	if err != nil {
		return dsnIdentity{}, err
	}
	db := strings.TrimPrefix(u.Path, "/")
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	host, port := hostPort(u)
	return dsnIdentity{host: host, port: port, db: db, user: user}, nil
}

func lookupPermissionCache(cfg PermissionCacheConfig, key string, now time.Time) (permissionCacheEntry, bool, error) {
	if !cfg.Enabled || cfg.Path == "" {
		return permissionCacheEntry{}, false, nil
	}
	doc, err := loadPermissionCacheFile(cfg.Path)
	if err != nil {
		return permissionCacheEntry{}, false, err
	}
	for _, e := range doc.Entries {
		if e.Key != key {
			continue
		}
		if now.Before(e.ExpiresAt) {
			return e, true, nil
		}
	}
	return permissionCacheEntry{}, false, nil
}

func storePermissionCache(cfg PermissionCacheConfig, entry permissionCacheEntry) error {
	if !cfg.Enabled || cfg.Path == "" {
		return nil
	}
	doc, err := loadPermissionCacheFile(cfg.Path)
	if err != nil {
		return err
	}
	now := permissionCacheNow()
	out := make([]permissionCacheEntry, 0, len(doc.Entries)+1)
	for _, e := range doc.Entries {
		if e.Key == entry.Key || !now.Before(e.ExpiresAt) {
			continue
		}
		out = append(out, e)
	}
	out = append(out, entry)
	if err := savePermissionCacheFile(cfg.Path, permissionCacheDoc{Entries: out}); err != nil {
		warnPermissionCache(fmt.Sprintf("dolly: warning: permission cache persist failed: %s: %v", cfg.Path, err))
		return nil
	}
	return nil
}

func loadPermissionCacheFromPath(path string) (permissionCacheDoc, error) {
	if err := ensureCacheOwnerOnly(path); err != nil {
		return permissionCacheDoc{}, fmt.Errorf("tighten permission cache: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return permissionCacheDoc{}, nil
		}
		return permissionCacheDoc{}, fmt.Errorf("read permission cache: %w", err)
	}
	var doc permissionCacheDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return permissionCacheDoc{}, fmt.Errorf("parse permission cache: %w", err)
	}
	return doc, nil
}

func savePermissionCacheToPath(path string, doc permissionCacheDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create permission cache dir: %w", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal permission cache: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dolly.permissions-cache-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp cache file: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	cleanup := func() {
		if committed {
			return
		}
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	defer cleanup()
	if err := ensureCacheOwnerOnly(tmpPath); err != nil {
		return fmt.Errorf("tighten permission cache temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write permission cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync permission cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close permission cache: %w", err)
	}
	if err := replacePermissionCacheFile(tmpPath, path); err != nil {
		return fmt.Errorf("rename permission cache: %w", err)
	}
	committed = true
	return nil
}

// NewPermissionCacheConfig builds cache settings from config.jsonc clone.preflight fields.
func NewPermissionCacheConfig(enabled bool, path, ttlRaw string) (PermissionCacheConfig, error) {
	if !enabled {
		return PermissionCacheConfig{}, nil
	}
	if path == "" {
		path = ".dolly/permissions-cache.yaml"
	}
	ttl := 24 * time.Hour
	if ttlRaw != "" {
		d, err := time.ParseDuration(ttlRaw)
		if err != nil {
			return PermissionCacheConfig{}, fmt.Errorf("parse cache_permissions_ttl: %w", err)
		}
		if d <= 0 {
			return PermissionCacheConfig{}, fmt.Errorf("cache_permissions_ttl must be positive")
		}
		ttl = d
	}
	return PermissionCacheConfig{Enabled: true, Path: path, TTL: ttl}, nil
}

func buildPermissionCacheEntry(
	key string,
	dsns preflightDSNs,
	opts Options,
	strategy, role string,
	ttl time.Duration,
) (permissionCacheEntry, error) {
	src, err := parseDSNIdentity(dsns.sourceDSN)
	if err != nil {
		return permissionCacheEntry{}, err
	}
	tgt, err := parseDSNIdentity(dsns.targetDSN)
	if err != nil {
		return permissionCacheEntry{}, err
	}
	now := permissionCacheNow()
	return permissionCacheEntry{
		Key:        key,
		Strategy:   strategy,
		SkipCreate: opts.SkipCreate,
		CloneName:  opts.CloneName,
		SourceDB:   src.db,
		SourceUser: src.user,
		SourceHost: src.host,
		SourcePort: src.port,
		TargetHost: tgt.host,
		TargetPort: tgt.port,
		TargetDB:   tgt.db,
		SameInst:   dsns.sameInst,
		Role:       role,
		CheckedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}, nil
}

var warnPermissionCache = func(msg string) { fmt.Fprintln(os.Stderr, msg) }
