package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Status identifies the update outcome.
type Status string

const (
	StatusAvailable Status = "available"
	StatusCurrent   Status = "current"
	StatusUpdated   Status = "updated"
	StatusDeferred  Status = "deferred"
	StatusFailed    Status = "failed"
)

// Result is the machine-readable update outcome.
type Result struct {
	OK               bool   `json:"ok"`
	Command          string `json:"command"`
	Status           Status `json:"status"`
	InstalledVersion string `json:"installed_version,omitempty"`
	RemoteVersion    string `json:"remote_version,omitempty"`
	Asset            string `json:"asset,omitempty"`
	Target           string `json:"target,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Options configures update discovery and verification.
type Options struct {
	HTTP             HTTPDoer
	Repo             string
	InstalledVersion string
	TargetPath       string
	CheckOnly        bool
	Now              func() time.Time
}

// Run discovers, compares, downloads, verifies, and optionally replaces the executable.
func Run(ctx context.Context, opts Options) (*Result, error) {
	repo := opts.Repo
	if repo == "" {
		repo = defaultRepo
	}

	target, err := resolveReplaceTarget(opts.TargetPath)
	if err != nil {
		return failedResult("%v", err), err
	}

	local, err := ParseVersion(opts.InstalledVersion)
	if err != nil {
		return failedResult("%v", err), err
	}

	if result, consumed := consumePendingReport(filepath.Dir(target), target, local.String()); consumed {
		if !result.OK {
			return result, fmt.Errorf("%s", result.Error)
		}
		return result, nil
	}

	assetName, err := CurrentAsset()
	if err != nil {
		return failedResult("%v", err), err
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}
	deadline := opts.Now().Add(overallTimeout)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	release, err := fetchLatestRelease(ctx, opts.HTTP, repo, assetName)
	if err != nil {
		return failedResult("%v", err), err
	}

	if !IsNewer(local, release.Version) {
		return &Result{
			OK:               true,
			Command:          "update",
			Status:           StatusCurrent,
			InstalledVersion: local.String(),
			RemoteVersion:    release.Version.String(),
			Asset:            assetName,
			Target:           target,
		}, nil
	}

	checksumsData, err := downloadAsset(ctx, opts.HTTP, release.ChecksumsAsset.BrowserDownloadURL, maxChecksumsBody)
	if err != nil {
		return failedResult("%v", err), err
	}
	wantSHA, err := parseChecksums(checksumsData, assetName)
	if err != nil {
		return failedResult("%v", err), err
	}

	archiveData, err := downloadAsset(ctx, opts.HTTP, release.Asset.BrowserDownloadURL, maxArchiveBody)
	if err != nil {
		return failedResult("%v", err), err
	}
	if err := verifyArchiveSHA256(archiveData, wantSHA); err != nil {
		return failedResult("%v", err), err
	}

	stageDir := filepath.Dir(target)
	if opts.CheckOnly {
		stageDir, err = os.MkdirTemp("", "dolly-update-check-*")
		if err != nil {
			return failedResult("create check staging dir: %v", err), fmt.Errorf("create check staging dir: %w", err)
		}
		defer os.RemoveAll(stageDir)
	}

	stagedPath, stagedSHA, err := extractAndStage(archiveData, assetName, runtime.GOOS, stageDir)
	if err != nil {
		return failedResult("%v", err), err
	}

	if opts.CheckOnly {
		defer os.Remove(stagedPath)
		return &Result{
			OK:               true,
			Command:          "update",
			Status:           StatusAvailable,
			InstalledVersion: local.String(),
			RemoteVersion:    release.Version.String(),
			Asset:            assetName,
			Target:           target,
		}, nil
	}

	oldSHA, oldSize, err := fileDigest(target)
	if err != nil {
		return failedResult("%v", err), err
	}

	status, err := applyReplacement(replacementInput{
		Target:        target,
		Candidate:     stagedPath,
		CandidateSHA:  stagedSHA,
		OldSHA:        oldSHA,
		OldSize:       oldSize,
		RemoteVersion: release.Version.String(),
	})
	if err != nil {
		return failedResult("%v", err), err
	}

	result := &Result{
		OK:               true,
		Command:          "update",
		Status:           status,
		InstalledVersion: local.String(),
		RemoteVersion:    release.Version.String(),
		Asset:            assetName,
		Target:           target,
	}
	if status == StatusUpdated {
		result.InstalledVersion = release.Version.String()
	}
	return result, nil
}

func failedResult(format string, args ...any) *Result {
	return &Result{
		OK:      false,
		Command: "update",
		Status:  StatusFailed,
		Error:   fmt.Sprintf(format, args...),
	}
}
