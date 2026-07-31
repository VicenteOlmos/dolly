//go:build windows

package config

// dotenvPermissionAdvisory is a quiet no-op on Windows where Unix
// owner/group/other mode semantics do not apply.
func dotenvPermissionAdvisory(string) (bool, error) {
	return false, nil
}

// ensureOwnerOnly is a no-op on Windows where Unix owner/group/other mode
// semantics do not apply.
func ensureOwnerOnly(path string) error {
	return ensureOwnerOnlyImpl(path)
}

var ensureOwnerOnlyImpl = func(string) error { return nil }
