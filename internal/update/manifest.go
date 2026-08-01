package update

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type updateManifest struct {
	Capability    string `json:"capability"`
	ParentPID     int    `json:"parent_pid"`
	Target        string `json:"target"`
	Candidate     string `json:"candidate"`
	Backup        string `json:"backup"`
	Helper        string `json:"helper"`
	OldSHA256     string `json:"old_sha256"`
	NewSHA256     string `json:"new_sha256"`
	OldSize       int64  `json:"old_size"`
	NewSize       int64  `json:"new_size"`
	RemoteVersion string `json:"remote_version"`
}

func newCapability() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate capability: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func writeManifest(path string, manifest updateManifest) error {
	payload, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("close manifest: %w", err)
	}
	return nil
}

func readManifest(path string) (updateManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return updateManifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest updateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return updateManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

func validCapability(capability string) bool {
	if len(capability) != 64 {
		return false
	}
	_, err := hex.DecodeString(capability)
	return err == nil
}

func validateManifest(manifest updateManifest, capability string) error {
	if manifest.ParentPID <= 0 {
		return fmt.Errorf("invalid parent pid %d", manifest.ParentPID)
	}
	if !validCapability(capability) || capability != manifest.Capability {
		return fmt.Errorf("invalid update capability")
	}
	paths := []string{manifest.Target, manifest.Candidate, manifest.Backup, manifest.Helper}
	if err := sameDirectory(paths...); err != nil {
		return err
	}
	for _, p := range paths {
		if p == "" {
			return fmt.Errorf("manifest path is empty")
		}
	}
	return validateExpectedManifestPaths(manifest)
}

func validateExpectedManifestPaths(manifest updateManifest) error {
	dir := filepath.Dir(manifest.Target)
	checks := map[string]string{
		manifest.Candidate: candidatePath(dir),
		manifest.Backup:    backupPath(dir),
		manifest.Helper:    helperPath(dir),
	}
	for got, want := range checks {
		canonGot, err := cleanAbsPath(got)
		if err != nil {
			return fmt.Errorf("resolve manifest path: %w", err)
		}
		canonWant, err := cleanAbsPath(want)
		if err != nil {
			return fmt.Errorf("resolve expected path: %w", err)
		}
		if canonGot != canonWant {
			return fmt.Errorf("unexpected manifest path %q", got)
		}
	}
	return nil
}

func validateManifestDigests(manifest updateManifest) error {
	oldSHA, oldSize, err := fileDigest(manifest.Target)
	if err != nil {
		return fmt.Errorf("verify target digest: %w", err)
	}
	if oldSHA != manifest.OldSHA256 || oldSize != manifest.OldSize {
		return fmt.Errorf("target digest mismatch")
	}
	newSHA, newSize, err := fileDigest(manifest.Candidate)
	if err != nil {
		return fmt.Errorf("verify candidate digest: %w", err)
	}
	if newSHA != manifest.NewSHA256 || newSize != manifest.NewSize {
		return fmt.Errorf("candidate digest mismatch")
	}
	return nil
}
