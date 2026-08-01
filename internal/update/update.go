package update

import "fmt"

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

func failedResult(format string, args ...any) *Result {
	return &Result{
		OK:      false,
		Command: "update",
		Status:  StatusFailed,
		Error:   fmt.Sprintf(format, args...),
	}
}
