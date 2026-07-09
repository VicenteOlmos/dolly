package restore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/VicenteOlmos/dolly/internal/dump"
)

func verifyNDJSONFiles(meta dump.Metadata, dir string) error {
	for _, table := range meta.Tables {
		path := ndjsonPath(dir, table.Name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("missing data file for table %q: %s", table.Name, path)
			}
			return fmt.Errorf("stat table %q data file: %w", table.Name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("data path for table %q is a directory: %s", table.Name, path)
		}
	}
	return nil
}

func ndjsonPath(dir, table string) string {
	return filepath.Join(dir, table+".ndjson")
}
