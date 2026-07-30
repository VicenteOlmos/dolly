package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadConfigReturnsDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.jsonc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env.Path != ".env" {
		t.Fatalf("expected Path=.env, got %q", cfg.Env.Path)
	}
	if cfg.Env.URLVar != "DB_URL" {
		t.Fatalf("expected URLVar=DB_URL, got %q", cfg.Env.URLVar)
	}
	if cfg.Env.HostVar != "DB_HOST" {
		t.Fatalf("expected HostVar=DB_HOST, got %q", cfg.Env.HostVar)
	}
	if cfg.Env.PortVar != "DB_PORT" {
		t.Fatalf("expected PortVar=DB_PORT, got %q", cfg.Env.PortVar)
	}
	if cfg.Env.NameVar != "DB_NAME" {
		t.Fatalf("expected NameVar=DB_NAME, got %q", cfg.Env.NameVar)
	}
	if cfg.Env.UserVar != "DB_USER" {
		t.Fatalf("expected UserVar=DB_USER, got %q", cfg.Env.UserVar)
	}
	if cfg.Env.PasswordVar != "DB_PASSWORD" {
		t.Fatalf("expected PasswordVar=DB_PASSWORD, got %q", cfg.Env.PasswordVar)
	}
	if cfg.Clone.NameTemplate != "{db}_dolly_{n}" {
		t.Fatalf("expected NameTemplate={db}_dolly_{n}, got %q", cfg.Clone.NameTemplate)
	}
	if cfg.Clone.RestoreOnConflict != "error" {
		t.Fatalf("expected RestoreOnConflict=error, got %q", cfg.Clone.RestoreOnConflict)
	}
	if cfg.Clone.Replace {
		t.Fatal("expected Replace=false")
	}
	if cfg.Clone.SkipCreate {
		t.Fatal("expected SkipCreate=false")
	}
	if cfg.Clone.Strategy != "schema-replay" {
		t.Fatalf("expected Strategy=schema-replay, got %q", cfg.Clone.Strategy)
	}
	if cfg.Sanitization.Enabled {
		t.Fatal("expected Sanitization.Enabled=false")
	}
	if cfg.Subset.Percent != 0 {
		t.Fatalf("expected Subset.Percent=0, got %d", cfg.Subset.Percent)
	}
	if cfg.SaveConnections {
		t.Fatal("expected SaveConnections=false")
	}
	if cfg.Connections.Scope != "project" {
		t.Fatalf("expected Connections.Scope=project, got %q", cfg.Connections.Scope)
	}
	if cfg.Connections.Encrypt {
		t.Fatal("expected Connections.Encrypt=false")
	}
	if cfg.TUI.Theme != "catppuccin-mocha" {
		t.Fatalf("expected TUI.Theme=catppuccin-mocha, got %q", cfg.TUI.Theme)
	}
}

// ── JSONC loading ─────────────────────────────────────────────────────────────

func TestLoadConfig_overlay(t *testing.T) {
	content := `{
  "clone": {
    "strategy": "template"
  }
}`
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Clone.Strategy != "template" {
		t.Fatalf("strategy = %q, want template", cfg.Clone.Strategy)
	}
	// Keys absent from the file retain defaults.
	if cfg.Env.Path != ".env" {
		t.Fatalf("env.path = %q, want .env (default)", cfg.Env.Path)
	}
	if cfg.Connections.Scope != "project" {
		t.Fatalf("connections.scope = %q, want project (default)", cfg.Connections.Scope)
	}
}

func TestLoadConfig_commentsAndTrailingCommas(t *testing.T) {
	content := `{
  // line comment
  "env": {
    "path": ".env.test", /* block comment */
  },
  "clone": {
    "strategy": "schema-replay", // trailing comma + comment
  }
}`
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env.Path != ".env.test" {
		t.Fatalf("env.path = %q, want .env.test", cfg.Env.Path)
	}
}

func TestLoadConfig_malformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(`{"bad": `), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(p)
	if err == nil {
		t.Fatal("expected error for malformed JSONC")
	}
	if !strings.Contains(err.Error(), "config.jsonc") {
		t.Fatalf("error should mention config.jsonc, got: %v", err)
	}
}

func TestLoadConfig_yamlPathErrors(t *testing.T) {
	dir := t.TempDir()
	cases := []string{".dolly.yaml", "config.yml"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte("env:\n  path: x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(p)
			if err == nil {
				t.Fatal("expected error for YAML config path")
			}
			if !strings.Contains(err.Error(), "config.jsonc") {
				t.Fatalf("error should mention config.jsonc, got: %v", err)
			}
		})
	}
}

func TestResolveConfigPath_ignoresYamlFiles(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if err := os.WriteFile(".dolly.yaml", []byte("env:\n  path: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveConfigPath(); got != "config.jsonc" {
		t.Fatalf("ResolveConfigPath() = %q, want config.jsonc", got)
	}
}

func TestConfigDBMaxOpenConnsDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DB.MaxOpenConns != 5 {
		t.Fatalf("DB.MaxOpenConns = %d, want 5", cfg.DB.MaxOpenConns)
	}
}

func TestConfigDBStatementTimeoutDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DB.StatementTimeout != "5min" {
		t.Fatalf("DB.StatementTimeout = %q, want 5min", cfg.DB.StatementTimeout)
	}
}

func TestLoadConfigDBMaxOpenConnsOverlay(t *testing.T) {
	content := `{
  "db": {
    "max_open_conns": 10
  }
}`
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DB.MaxOpenConns != 10 {
		t.Fatalf("DB.MaxOpenConns = %d, want 10", cfg.DB.MaxOpenConns)
	}
}

func TestBootstrapConfigWrites0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.jsonc")
	if err := BootstrapConfig(path); err != nil {
		t.Fatalf("BootstrapConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestSaveConfigTightensExistingFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(path, []byte(`{"clone":{"strategy":"template"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(DefaultConfig(), path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
}

func TestSaveConfigNoopTightensExistingFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(path, DefaultTemplate(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(cfg, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
}

func TestBootstrapConfig_idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	// First call writes the file.
	if err := BootstrapConfig(path); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	data1, err := os.ReadFile(path)
	if err != nil || len(data1) == 0 {
		t.Fatalf("file not written: %v", err)
	}

	// Overwrite file with sentinel content.
	sentinel := []byte("sentinel")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call must be a no-op.
	if err := BootstrapConfig(path); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data2, sentinel) {
		t.Fatal("BootstrapConfig overwrote existing file (should be no-op)")
	}
}

func TestSaveConfig(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")

		orig := DefaultConfig()
		orig.DB.StatementTimeout = "5min" // template default — match what gets round-tripped
		orig.Clone.Schemas = []string{}   // match JSON "schemas": [] after template-based save
		orig.Dump.Schemas = []string{}    // match JSON "dump.schemas": [] after template-based save
		orig.Clone.Strategy = "template"
		orig.Env.Path = ".env.custom"
		orig.Subset.Percent = 42

		if err := SaveConfig(orig, path); err != nil {
			t.Fatalf("SaveConfig: %v", err)
		}

		// File must contain valid JSON (JSONC comments allowed).
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		var check interface{}
		if err := json.Unmarshal(stripJSONC(raw), &check); err != nil {
			t.Fatalf("saved file is not valid JSON: %v", err)
		}

		// Round-trip via LoadConfig.
		got, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		if !reflect.DeepEqual(orig, got) {
			t.Fatalf("round-trip mismatch:\norig: %+v\ngot:  %+v", orig, got)
		}
	})

	t.Run("write-error returns error", func(t *testing.T) {
		badPath := filepath.Join(t.TempDir(), "no-such-dir", "config.json")
		cfg := DefaultConfig()
		cfg.Clone.Strategy = "template" // non-empty patch forces a write attempt
		err := SaveConfig(cfg, badPath)
		if err == nil {
			t.Fatal("expected error for non-writable path")
		}
	})
}

func TestSaveConfig_preservesJSONCComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	// Start from the documented template so comments are present.
	if err := os.WriteFile(path, DefaultTemplate(), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if !bytes.Contains(before, []byte("// Path to the .env file")) {
		t.Fatal("template missing expected comment")
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Clone.Strategy = "template"
	cfg.Env.Path = ".env.custom"

	if err := SaveConfig(cfg, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved: %v", err)
	}
	if !bytes.Contains(after, []byte("// Path to the .env file")) {
		t.Fatal("comment lost after SaveConfig")
	}
	if !bytes.Contains(after, []byte(`"strategy": "template"`)) {
		t.Fatalf("expected updated strategy in:\n%s", after)
	}
	if !bytes.Contains(after, []byte(`"path": ".env.custom"`)) {
		t.Fatalf("expected updated env.path in:\n%s", after)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig after save: %v", err)
	}
	if got.Clone.Strategy != "template" || got.Env.Path != ".env.custom" {
		t.Fatalf("reload mismatch: %+v", got)
	}
}

func TestSaveConfig_preserves0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	if err := os.WriteFile(path, DefaultTemplate(), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Clone.Strategy = "template"

	if err := SaveConfig(cfg, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestSaveConfigAtomicallyTightensExistingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(path, DefaultTemplate(), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Clone.Strategy = "template"
	if err := SaveConfig(cfg, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.jsonc.tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary config files remain: %v, %v", matches, err)
	}
}

func TestSaveConfigFailClosedBeforeBaselineRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.jsonc")
	content := `{"clone":{"strategy":"template"}}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := ensureOwnerOnlyImpl
	ensureOwnerOnlyImpl = func(string) error {
		return errors.New("injected tighten failure")
	}
	t.Cleanup(func() { ensureOwnerOnlyImpl = orig })

	cfg := DefaultConfig()
	cfg.Clone.Strategy = "template"
	err := SaveConfig(cfg, path)
	if err == nil {
		t.Fatal("expected error from injected tighten failure")
	}
	if !strings.Contains(err.Error(), "injected tighten failure") {
		t.Fatalf("expected tighten error, got %v", err)
	}
	if strings.Contains(err.Error(), "chmod config") || strings.Contains(err.Error(), "parse config") {
		t.Fatalf("baseline read/parse should not be reached before tighten: %v", err)
	}
}

func TestDefaultConfigDumpOutputDir(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Dump.OutputDir != "dolly_dump" {
		t.Fatalf("Dump.OutputDir = %q, want %q", cfg.Dump.OutputDir, "dolly_dump")
	}
}

func TestLoadConfigSectionEntryOverview(t *testing.T) {
	content := `{
  "tui": {
    "section_entry": "overview"
  }
}`
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TUI.SectionEntry != "overview" {
		t.Fatalf("TUI.SectionEntry = %q, want overview", cfg.TUI.SectionEntry)
	}
}

func TestLoadConfigDumpRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Dump.OutputDir = "custom_dump_dir"

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Dump.OutputDir != "custom_dump_dir" {
		t.Fatalf("Dump.OutputDir = %q, want %q", got.Dump.OutputDir, "custom_dump_dir")
	}
}

func TestLoadConfigDumpTableSelectionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Dump.IncludeTables = []string{"public.users", "app.events"}
	orig.Dump.ExcludeTables = []string{"public.audit_log"}
	orig.Dump.IncludeTableFiles = []string{"tables/include.txt"}
	orig.Dump.ExcludeTableFiles = []string{"tables/exclude.txt", "tables/more-exclude.txt"}

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(orig.Dump.IncludeTables, got.Dump.IncludeTables) {
		t.Fatalf("IncludeTables = %v, want %v", got.Dump.IncludeTables, orig.Dump.IncludeTables)
	}
	if !reflect.DeepEqual(orig.Dump.ExcludeTables, got.Dump.ExcludeTables) {
		t.Fatalf("ExcludeTables = %v, want %v", got.Dump.ExcludeTables, orig.Dump.ExcludeTables)
	}
	if !reflect.DeepEqual(orig.Dump.IncludeTableFiles, got.Dump.IncludeTableFiles) {
		t.Fatalf("IncludeTableFiles = %v, want %v", got.Dump.IncludeTableFiles, orig.Dump.IncludeTableFiles)
	}
	if !reflect.DeepEqual(orig.Dump.ExcludeTableFiles, got.Dump.ExcludeTableFiles) {
		t.Fatalf("ExcludeTableFiles = %v, want %v", got.Dump.ExcludeTableFiles, orig.Dump.ExcludeTableFiles)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	for _, want := range []string{
		`"include_tables": [`,
		`"public.users"`,
		`"exclude_tables": [`,
		`"public.audit_log"`,
		`"include_table_files": [`,
		`"tables/include.txt"`,
		`"exclude_table_files": [`,
		`"tables/more-exclude.txt"`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("saved config missing %q in:\n%s", want, raw)
		}
	}
}

func TestLoadConfigDumpChunkTablesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Dump.ChunkTables = []string{"public.orders", "public.events"}
	orig.Dump.ChunkTableFiles = []string{"tables/chunk.txt"}

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(orig.Dump.ChunkTables, got.Dump.ChunkTables) {
		t.Fatalf("ChunkTables = %v, want %v", got.Dump.ChunkTables, orig.Dump.ChunkTables)
	}
	if !reflect.DeepEqual(orig.Dump.ChunkTableFiles, got.Dump.ChunkTableFiles) {
		t.Fatalf("ChunkTableFiles = %v, want %v", got.Dump.ChunkTableFiles, orig.Dump.ChunkTableFiles)
	}
}

func TestLoadConfigDumpSchemasRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Dump.Schemas = []string{"app", "billing"}

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(orig.Dump.Schemas, got.Dump.Schemas) {
		t.Fatalf("Schemas = %v, want %v", got.Dump.Schemas, orig.Dump.Schemas)
	}
}

func TestLoadConfigDumpWorkersRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Dump.Workers = 4

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Dump.Workers != 4 {
		t.Fatalf("Dump.Workers = %d, want 4", got.Dump.Workers)
	}
}

func TestLoadConfigRestoreWorkersRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")

	orig := DefaultConfig()
	orig.Restore.Workers = 4
	orig.Restore.PartialStateFile = "/tmp/restore-state.json"

	if err := SaveConfig(orig, path); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Restore.Workers != 4 {
		t.Fatalf("Restore.Workers = %d, want 4", got.Restore.Workers)
	}
	if got.Restore.PartialStateFile != "/tmp/restore-state.json" {
		t.Fatalf("Restore.PartialStateFile = %q", got.Restore.PartialStateFile)
	}
}

func TestDriftGuard(t *testing.T) {
	// The embedded template and the committed config.jsonc at repo root must be identical.
	// This test is intentionally skipped when the repo root file is absent (e.g. in sub-tree builds).
	repoRoot := filepath.Join("..", "..", "config.jsonc")
	committed, err := os.ReadFile(repoRoot)
	if os.IsNotExist(err) {
		t.Skip("config.jsonc not found at repo root — skipping drift guard")
	}
	if err != nil {
		t.Fatalf("read repo config.jsonc: %v", err)
	}
	if !bytes.Equal(committed, defaultConfigTemplate) {
		t.Fatalf("config.jsonc and embedded template have drifted; regenerate config.jsonc from internal/config/config.jsonc.tmpl")
	}
}

func TestLoadConfigTightensBeforeRead(t *testing.T) {
	content := `{"clone":{"strategy":"template"}}`
	p := filepath.Join(t.TempDir(), "config.jsonc")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Clone.Strategy != "template" {
		t.Fatalf("strategy = %q, want template", cfg.Clone.Strategy)
	}
	if runtime.GOOS == "windows" {
		return // Windows no-op preserves original mode
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600 after tighten", got)
	}
}
