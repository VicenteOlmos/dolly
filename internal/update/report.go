package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type pendingReport struct {
	Status        Status `json:"status"`
	RemoteVersion string `json:"remote_version,omitempty"`
	Error         string `json:"error,omitempty"`
}

func consumePendingReport(dir, target, installedVersion string) (*Result, bool) {
	path := reportPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		return failedResult("read update report: %v", err), true
	}
	_ = os.Remove(path)

	var report pendingReport
	if err := json.Unmarshal(data, &report); err != nil {
		return failedResult("decode update report: %v", err), true
	}
	if report.Status != StatusUpdated && report.Status != StatusFailed {
		return failedResult("invalid update report status %q", report.Status), true
	}

	result := &Result{
		OK:               report.Status == StatusUpdated,
		Command:          "update",
		Status:           report.Status,
		InstalledVersion: installedVersion,
		RemoteVersion:    report.RemoteVersion,
		Target:           target,
	}
	if report.Error != "" {
		result.Error = report.Error
	}
	if report.Status == StatusFailed {
		return result, true
	}
	return result, true
}

func writePendingReport(dir string, report pendingReport) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal update report: %w", err)
	}
	tmp := reportTempPath(dir)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("write update report temp: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write update report temp: %w", err)
	}
	if err := syncOpenFile(f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsync update report temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close update report temp: %w", err)
	}
	final := reportPath(dir)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish update report: %w", err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("fsync update report directory: %w", err)
	}
	return nil
}

func sameDirectory(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	base, err := filepath.Abs(filepath.Dir(paths[0]))
	if err != nil {
		return fmt.Errorf("resolve directory: %w", err)
	}
	for _, p := range paths[1:] {
		dir, err := filepath.Abs(filepath.Dir(p))
		if err != nil {
			return fmt.Errorf("resolve directory: %w", err)
		}
		if dir != base {
			return fmt.Errorf("paths must share the same directory")
		}
	}
	return nil
}
