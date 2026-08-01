package update

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const helperWaitTimeout = 2 * time.Minute

// RunCleanup removes helper artifacts and publishes the deferred update report.
func RunCleanup(manifestFile, capability string) error {
	manifest, err := readManifest(manifestFile)
	if err != nil {
		return err
	}
	if err := validateManifest(manifest, capability); err != nil {
		return err
	}
	if err := validateManifestFilePath(manifestFile, manifest); err != nil {
		return err
	}
	if err := validateUpdatedTarget(manifest); err != nil {
		return err
	}

	parent := cleanupWaitParentPID()
	if err := waitForPIDExit(parent, helperWaitTimeout); err != nil {
		return err
	}

	dir := filepath.Dir(manifest.Target)
	for _, path := range authenticatedTempPaths(manifest, manifestFile) {
		_ = os.Remove(path)
	}

	return writePendingReport(dir, pendingReport{
		Status:        StatusUpdated,
		RemoteVersion: manifest.RemoteVersion,
	})
}

func validateManifestFilePath(manifestFile string, manifest updateManifest) error {
	canonManifest, err := cleanAbsPath(manifestFile)
	if err != nil {
		return fmt.Errorf("resolve manifest path: %w", err)
	}
	canonExpected, err := cleanAbsPath(manifestPath(filepath.Dir(manifest.Target)))
	if err != nil {
		return fmt.Errorf("resolve expected manifest path: %w", err)
	}
	if canonManifest != canonExpected {
		return fmt.Errorf("invalid manifest path")
	}
	return nil
}

func validateUpdatedTarget(manifest updateManifest) error {
	sha, size, err := fileDigest(manifest.Target)
	if err != nil {
		return fmt.Errorf("verify updated target: %w", err)
	}
	if sha != manifest.NewSHA256 || size != manifest.NewSize {
		return fmt.Errorf("updated target digest mismatch")
	}
	return nil
}

func authenticatedTempPaths(manifest updateManifest, manifestFile string) []string {
	return []string{manifest.Backup, manifest.Helper, manifest.Candidate, manifestFile}
}
