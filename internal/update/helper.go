package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const helperWaitTimeout = 2 * time.Minute

var renameFile = os.Rename

// RunHelper performs the deferred Windows swap after the parent exits.
func RunHelper(manifestFile, capability string) error {
	manifest, err := readManifest(manifestFile)
	if err != nil {
		return err
	}
	if err := validateManifest(manifest, capability); err != nil {
		return err
	}
	if err := validateManifestDigests(manifest); err != nil {
		return writeHelperFailure(manifest, err)
	}

	if err := waitForPIDExit(manifest.ParentPID, helperWaitTimeout); err != nil {
		return writeHelperFailure(manifest, err)
	}

	if err := renameFile(manifest.Target, manifest.Backup); err != nil {
		return writeHelperFailure(manifest, restoreAfterFailure(manifest, err))
	}
	if err := renameFile(manifest.Candidate, manifest.Target); err != nil {
		return writeHelperFailure(manifest, restoreAfterFailure(manifest, err))
	}

	afterSHA, _, err := fileDigest(manifest.Target)
	if err != nil || afterSHA != manifest.NewSHA256 {
		return writeHelperFailure(manifest, restoreAfterFailure(manifest, fmt.Errorf("updated target digest mismatch")))
	}

	cleanupArgv := []string{manifest.Target, "__update-cleanup", manifestFile, capability}
	if err := startDetachedProcess(manifest.Target, cleanupArgv); err != nil {
		return writeHelperFailure(manifest, restoreAfterFailure(manifest, err))
	}
	return nil
}

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

func restoreAfterFailure(manifest updateManifest, cause error) error {
	if manifest.Backup != "" {
		if _, err := os.Stat(manifest.Backup); err == nil {
			_ = renameFile(manifest.Backup, manifest.Target)
		}
	}
	_ = os.Remove(manifest.Candidate)
	_ = os.Remove(manifest.Helper)
	_ = os.Remove(manifestFilePath(manifest))
	return cause
}

func writeHelperFailure(manifest updateManifest, cause error) error {
	cause = restoreAfterFailure(manifest, cause)
	dir := filepath.Dir(manifest.Target)
	reportErr := writePendingReport(dir, pendingReport{
		Status:        StatusFailed,
		RemoteVersion: manifest.RemoteVersion,
		Error:         cause.Error(),
	})
	if reportErr != nil {
		return errors.Join(cause, reportErr)
	}
	return cause
}

func manifestFilePath(manifest updateManifest) string {
	return manifestPath(filepath.Dir(manifest.Target))
}
