//go:build windows

package config

// ensureOwnerOnly is a no-op on Windows where Unix owner/group/other mode
// semantics do not apply.
func ensureOwnerOnly(path string) error {
	return ensureOwnerOnlyImpl(path)
}

var ensureOwnerOnlyImpl = func(string) error { return nil }
