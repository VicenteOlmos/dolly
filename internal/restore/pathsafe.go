package restore

import (
	"path"
	"path/filepath"
	"strings"
)

func validateDeclaredDataFilePath(rel string) bool {
	if rel == "" || rel == "." || rel == ".." {
		return false
	}
	if filepath.IsAbs(rel) {
		return false
	}
	if strings.Contains(rel, `\`) {
		return false
	}
	clean := path.Clean(rel)
	if clean != rel {
		return false
	}
	if strings.HasPrefix(clean, "../") || clean == ".." {
		return false
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func pathContainedInRoot(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if rootResolved, err := filepath.EvalSymlinks(root); err == nil {
		root = rootResolved
	}
	if targetResolved, err := filepath.EvalSymlinks(target); err == nil {
		target = targetResolved
	} else {
		target = filepath.Clean(target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
