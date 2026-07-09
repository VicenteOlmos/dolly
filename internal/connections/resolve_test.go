package connections

import (
	"path/filepath"
	"testing"

	"github.com/VicenteOlmos/dolly/internal/config"
)

func TestResolveDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	_, err := Resolve(cfg, t.TempDir(), "staging")
	if err == nil {
		t.Fatal("expected error when save_connections is false")
	}
}

func TestResolveEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	t.Setenv("DOLLY_CONNECTIONS_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	dir := t.TempDir()
	store, err := OpenStore(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(sampleConnection("staging")); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(cfg, dir, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "db.example.com" {
		t.Fatalf("unexpected connection: %+v", got)
	}
}

func TestOpenStoreEncryptFailsClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SaveConnections = true
	cfg.Connections.Encrypt = true
	_, err := OpenStore(cfg, filepath.Join(t.TempDir()))
	if err != ErrEncryptKey {
		t.Fatalf("expected ErrEncryptKey, got %v", err)
	}
}
