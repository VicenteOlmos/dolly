package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

func verifyNDJSONFiles(meta dump.Metadata, dir string) ([]string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve input directory: %w", err)
	}
	paths := make([]string, len(meta.Tables))
	seen := make(map[string]struct{}, len(meta.Tables))
	for i, table := range meta.Tables {
		path, err := resolveDataFile(root, table)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[path]; ok {
			return nil, fmt.Errorf("duplicate data file for table %q", table.Name)
		}
		seen[path] = struct{}{}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("missing data file for table %q: %s", table.Name, path)
			}
			return nil, fmt.Errorf("stat table %q data file: %w", table.Name, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("data path for table %q is a directory: %s", table.Name, path)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !within(root, resolved) {
			return nil, fmt.Errorf("unsafe data file for table %q", table.Name)
		}
		paths[i] = path
	}
	return paths, nil
}

func resolveDataFile(root string, table db.Table) (string, error) {
	path := table.Name + ".ndjson"
	if table.DataFile != nil {
		path = *table.DataFile
		if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || filepath.Clean(path) != path || path == "." || path == ".." {
			return "", fmt.Errorf("unsafe data file for table %q", table.Name)
		}
	}
	full := filepath.Join(root, path)
	if !within(root, full) {
		return "", fmt.Errorf("unsafe data file for table %q", table.Name)
	}
	return full, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
