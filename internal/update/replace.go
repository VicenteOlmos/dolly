package update

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolveReplaceTarget(path string) (string, error) {
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve executable path: %w", err)
		}
		path = exe
	}
	if _, err := validateReplaceTarget(path); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	if _, err := validateReplaceTarget(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func validateReplaceTarget(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat target: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("target %q is a symlink; reinstall from a regular file path", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("target %q is not a regular file", path)
	}
	return info, nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return filepath.Clean(abs), nil
	}
	return resolved, nil
}

func cleanAbsPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
