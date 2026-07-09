package connections

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestResolveStorePathProjectDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cwd := t.TempDir()

	got, err := ResolveStorePath(cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".dolly.connections.yaml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathCustomOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Connections.Path = "secrets/profiles.yaml"
	cwd := t.TempDir()

	got, err := ResolveStorePath(cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "secrets", "profiles.yaml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveStorePathXDG(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Connections.Scope = "xdg"
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, err := ResolveStorePath(cfg, "/any/cwd")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "dolly", "connections.yaml")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOpenStoreXDGPersists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cfg.Connections.Scope = "xdg"
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	store, err := OpenStore(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sampleConnection("prod")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(xdg, "dolly", "connections.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("xdg store file: %v", err)
	}
}
