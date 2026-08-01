package update

import (
	"path/filepath"
	"runtime"
)

const (
	candidateBaseName = ".dolly-update-candidate"
	backupBaseName    = ".dolly-update-backup"
	helperBaseName    = ".dolly-update-helper"
	manifestBaseName  = ".dolly-update-manifest.json"
	reportBaseName    = ".dolly-update-report.json"
	reportTempBase    = ".dolly-update-report.json.tmp"
)

func candidatePath(dir string) string {
	return filepath.Join(dir, candidateBaseName)
}

func backupPath(dir string) string {
	name := backupBaseName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func helperPath(dir string) string {
	name := helperBaseName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name)
}

func manifestPath(dir string) string {
	return filepath.Join(dir, manifestBaseName)
}

func reportPath(dir string) string {
	return filepath.Join(dir, reportBaseName)
}

func reportTempPath(dir string) string {
	return filepath.Join(dir, reportTempBase)
}
