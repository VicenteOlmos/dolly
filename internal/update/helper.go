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
		return failHelperEarly(manifest, manifestFile, err)
	}

	if err := waitForPIDExit(manifest.ParentPID, helperWaitTimeout); err != nil {
		return failHelperEarly(manifest, manifestFile, err)
	}

	if err := validateManifestDigests(manifest); err != nil {
		return failHelperEarly(manifest, manifestFile, err)
	}

	if err := removeStaleBackup(manifest); err != nil {
		return failHelperEarly(manifest, manifestFile, err)
	}

	if err := renameFile(manifest.Target, manifest.Backup); err != nil {
		return handleHelperFailure(manifest, manifestFile, err, false)
	}

	backupTrusted, err := verifyTrustedBackup(manifest)
	if err != nil {
		return handleHelperFailure(manifest, manifestFile, err, false)
	}

	if err := renameFile(manifest.Candidate, manifest.Target); err != nil {
		return handleHelperFailure(manifest, manifestFile, err, backupTrusted)
	}

	afterSHA, afterSize, err := fileDigest(manifest.Target)
	if err != nil {
		return handleHelperFailure(manifest, manifestFile, fmt.Errorf("verify updated target: %w", err), backupTrusted)
	}
	if afterSHA != manifest.NewSHA256 || afterSize != manifest.NewSize {
		return handleHelperFailure(manifest, manifestFile, fmt.Errorf("updated target digest mismatch"), backupTrusted)
	}

	cleanupArgv := []string{manifest.Target, "__update-cleanup", manifestFile, capability}
	if err := startDetachedProcess(manifest.Target, cleanupArgv); err != nil {
		return handleHelperFailure(manifest, manifestFile, err, backupTrusted)
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

func removeStaleBackup(manifest updateManifest) error {
	if manifest.Backup == "" {
		return nil
	}
	if _, err := os.Stat(manifest.Backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Remove(manifest.Backup)
}

func verifyTrustedBackup(manifest updateManifest) (bool, error) {
	sha, size, err := fileDigest(manifest.Backup)
	if err != nil {
		return false, fmt.Errorf("verify backup digest: %w", err)
	}
	if sha != manifest.OldSHA256 || size != manifest.OldSize {
		return false, fmt.Errorf("backup digest mismatch")
	}
	return true, nil
}

func verifyRestoredTarget(manifest updateManifest) error {
	sha, size, err := fileDigest(manifest.Target)
	if err != nil {
		return fmt.Errorf("verify restored target: %w", err)
	}
	if sha != manifest.OldSHA256 || size != manifest.OldSize {
		return fmt.Errorf("restored target digest mismatch")
	}
	return nil
}

func targetAtOldDigest(manifest updateManifest) bool {
	sha, size, err := fileDigest(manifest.Target)
	if err != nil {
		return false
	}
	return sha == manifest.OldSHA256 && size == manifest.OldSize
}

func cleanupSafeHelperArtifacts(manifest updateManifest, manifestFile string) {
	_ = os.Remove(manifest.Candidate)
	_ = os.Remove(manifest.Helper)
	if manifestFile != "" {
		_ = os.Remove(manifestFile)
	} else {
		_ = os.Remove(manifestFilePath(manifest))
	}
}

func failHelperEarly(manifest updateManifest, manifestFile string, primary error) error {
	if targetAtOldDigest(manifest) {
		cleanupSafeHelperArtifacts(manifest, manifestFile)
	}
	return publishHelperFailureReport(manifest, primary, nil)
}

func handleHelperFailure(manifest updateManifest, manifestFile string, primary error, backupTrusted bool) error {
	var rollbackErr error
	preserveArtifacts := false

	if backupTrusted {
		rollbackErr = renameFile(manifest.Backup, manifest.Target)
		if rollbackErr == nil {
			rollbackErr = verifyRestoredTarget(manifest)
		}
		if rollbackErr != nil {
			preserveArtifacts = true
		}
	} else if !targetAtOldDigest(manifest) {
		preserveArtifacts = true
	}

	if !preserveArtifacts {
		cleanupSafeHelperArtifacts(manifest, manifestFile)
	}

	return publishHelperFailureReport(manifest, primary, rollbackErr)
}

func publishHelperFailureReport(manifest updateManifest, primary, rollback error) error {
	var cause error
	switch {
	case primary != nil && rollback != nil:
		cause = errors.Join(primary, rollback)
	case rollback != nil:
		cause = rollback
	default:
		cause = primary
	}

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
