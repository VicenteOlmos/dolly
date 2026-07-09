package connections

import (
	"fmt"
	"os"

	"github.com/VicenteOlmos/dolly/internal/config"
)

// OpenStore returns a FileStore when save_connections is enabled, or nil when disabled.
// When connections.encrypt is false but save_connections is enabled,
// encrypt is defaulted to true with a warning on stderr. Explicit encrypt: true
// requires DOLLY_CONNECTIONS_KEY to be set (32-byte key, standard base64).
func OpenStore(cfg *config.Config, cwd string) (ConnectionStore, error) {
	if cfg == nil || !cfg.SaveConnections {
		return nil, nil
	}
	path, err := ResolveStorePath(cfg, cwd)
	if err != nil {
		return nil, err
	}
	encrypt := cfg.Connections.Encrypt
	if !encrypt {
		fmt.Fprintf(os.Stderr, "connections.encrypt not set; defaulting to true when save_connections is enabled\n")
		encrypt = true
	} else {
		if _, err := loadEncryptionKey(); err != nil {
			return nil, err
		}
	}
	return NewFileStore(path, encrypt)
}

// Resolve loads a named profile when save_connections is enabled.
func Resolve(cfg *config.Config, cwd, name string) (Connection, error) {
	if cfg == nil || !cfg.SaveConnections {
		return Connection{}, fmt.Errorf("save_connections is disabled; enable it in config.jsonc to use saved connections")
	}
	store, err := OpenStore(cfg, cwd)
	if err != nil {
		return Connection{}, err
	}
	if store == nil {
		return Connection{}, fmt.Errorf("save_connections is disabled")
	}
	return store.Get(name)
}
