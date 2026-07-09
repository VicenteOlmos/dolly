package dumphistory

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/VicenteOlmos/dolly/internal/config"
)

const defaultHistoryFile = ".dolly/dump-history.json"

// ResolveStorePath returns the absolute path to the dump history JSON store.
func ResolveStorePath(cfg *config.Config, cwd string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is nil")
	}
	_ = cfg // reserved for future dump.history_path config
	return filepath.Join(cwd, defaultHistoryFile), nil
}

// OpenStore opens the default dump history store for the working directory.
func OpenStore(cfg *config.Config, cwd string) (*FileStore, error) {
	path, err := ResolveStorePath(cfg, cwd)
	if err != nil {
		return nil, err
	}
	return NewFileStore(path)
}

// DirSize sums file sizes under dir (best-effort).
func DirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}
