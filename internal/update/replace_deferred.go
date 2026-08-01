package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type deferredLayout struct {
	Target    string
	Candidate string
	Backup    string
	Helper    string
	Manifest  string
}

func deferredLayoutFor(dir string, goos string) deferredLayout {
	suffix := ""
	if goos == "windows" {
		suffix = ".exe"
	}
	return deferredLayout{
		Target:    filepath.Join(dir, "dolly"+suffix),
		Candidate: filepath.Join(dir, candidateBaseName),
		Backup:    filepath.Join(dir, backupBaseName+suffix),
		Helper:    filepath.Join(dir, helperBaseName+suffix),
		Manifest:  filepath.Join(dir, manifestBaseName),
	}
}

func helperArgv(helper, manifestFile, capability string) []string {
	return []string{helper, "__update-helper", manifestFile, capability}
}

func cleanupArgv(target, manifestFile, capability string) []string {
	return []string{target, "__update-cleanup", manifestFile, capability}
}

type deferredPrepOptions struct {
	GOOS      string
	ParentPID int
	CopyExec  func(dst string) error
	Launch    func(path string, argv []string) error
}

func defaultDeferredPrepOptions() deferredPrepOptions {
	return deferredPrepOptions{
		GOOS:      "windows",
		ParentPID: os.Getpid(),
		CopyExec:  copyExecutableExclusive,
		Launch:    startDetachedProcess,
	}
}

func prepareDeferredReplacement(input replacementInput, opts deferredPrepOptions) (Status, error) {
	if opts.GOOS == "" {
		opts.GOOS = "windows"
	}
	if opts.ParentPID == 0 {
		opts.ParentPID = os.Getpid()
	}
	if opts.CopyExec == nil {
		opts.CopyExec = copyExecutableExclusive
	}
	if opts.Launch == nil {
		opts.Launch = startDetachedProcess
	}

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
	dir := filepath.Dir(target)
	if err := sameDirectory(target, candidate); err != nil {
		return StatusFailed, err
	}
	layout := deferredLayoutFor(dir, opts.GOOS)
	canonCandidate, err := canonicalPath(layout.Candidate)
	if err != nil {
		return StatusFailed, fmt.Errorf("resolve candidate path: %w", err)
	}
	if candidate != canonCandidate {
		return StatusFailed, fmt.Errorf("candidate must be staged beside target")
	}
	if err := validateManifestDigests(updateManifest{
		Target:    target,
		Candidate: candidate,
		OldSHA256: input.OldSHA,
		NewSHA256: input.CandidateSHA,
		OldSize:   input.OldSize,
		NewSize:   mustFileSize(candidate),
	}); err != nil {
		return StatusFailed, err
	}

	capability, err := newCapability()
	if err != nil {
		return StatusFailed, err
	}

	helper := layout.Helper
	if err := opts.CopyExec(helper); err != nil {
		return StatusFailed, err
	}

	manifest := updateManifest{
		Capability:    capability,
		ParentPID:     opts.ParentPID,
		Target:        target,
		Candidate:     candidate,
		Backup:        layout.Backup,
		Helper:        helper,
		OldSHA256:     input.OldSHA,
		NewSHA256:     input.CandidateSHA,
		OldSize:       input.OldSize,
		NewSize:       mustFileSize(candidate),
		RemoteVersion: input.RemoteVersion,
	}
	manifestFile := layout.Manifest
	if err := writeManifest(manifestFile, manifest); err != nil {
		_ = os.Remove(helper)
		return StatusFailed, err
	}

	if err := opts.Launch(helper, helperArgv(helper, manifestFile, capability)); err != nil {
		_ = os.Remove(helper)
		_ = os.Remove(manifestFile)
		return StatusFailed, fmt.Errorf("start update helper: %w", err)
	}
	return StatusDeferred, nil
}

func copyExecutableExclusive(dst string) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open executable: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return fmt.Errorf("create helper copy: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("copy helper: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("close helper copy: %w", err)
	}
	return nil
}

func mustFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
