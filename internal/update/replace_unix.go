//go:build !windows

package update

import (
	"fmt"
	"os"
	"path/filepath"
)

type replacementInput struct {
	Target        string
	Candidate     string
	CandidateSHA  string
	OldSHA        string
	OldSize       int64
	RemoteVersion string
}

func applyReplacement(input replacementInput) (Status, error) {
	return applyReplacementUnix(input)
}

func applyReplacementUnix(input replacementInput) (Status, error) {
	if _, err := validateReplaceTarget(input.Target); err != nil {
		return StatusFailed, err
	}

	target, err := canonicalPath(input.Target)
	if err != nil {
		return StatusFailed, fmt.Errorf("resolve target path: %w", err)
	}
	candidate, err := canonicalPath(input.Candidate)
	if err != nil {
		return StatusFailed, fmt.Errorf("resolve candidate path: %w", err)
	}
	if err := sameDirectory(target, candidate); err != nil {
		return StatusFailed, err
	}

	info, err := validateReplaceTarget(target)
	if err != nil {
		return StatusFailed, err
	}

	gotSHA, _, err := fileDigest(candidate)
	if err != nil {
		return StatusFailed, err
	}
	if gotSHA != input.CandidateSHA {
		return StatusFailed, fmt.Errorf("candidate digest mismatch")
	}

	mode := info.Mode().Perm()
	if err := os.Chmod(candidate, mode); err != nil {
		return StatusFailed, permissionRemediation(err)
	}
	if err := syncFile(candidate); err != nil {
		return StatusFailed, fmt.Errorf("fsync candidate: %w", err)
	}
	if err := os.Rename(candidate, target); err != nil {
		return StatusFailed, permissionRemediation(err)
	}
	if err := syncDir(filepath.Dir(target)); err != nil {
		return StatusFailed, fmt.Errorf("fsync target directory: %w", err)
	}

	afterSHA, _, err := fileDigest(target)
	if err != nil {
		return StatusFailed, fmt.Errorf("verify updated target: %w", err)
	}
	if afterSHA != input.CandidateSHA {
		return StatusFailed, fmt.Errorf("updated target digest mismatch")
	}
	return StatusUpdated, nil
}

func permissionRemediation(err error) error {
	if os.IsPermission(err) {
		return fmt.Errorf("%w; reinstall with sudo or install to a user-writable path", err)
	}
	return err
}
