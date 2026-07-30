package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/connections"
)

const (
	partialStateFileMode = 0o600
	partialStateDirMode  = 0o700
	defaultPartialState  = ".dolly-restore-partial-state.json"
	maxFailureDetailLen  = 512
)

// ErrPartialStatePath marks an unsafe partial-state manifest path.
var ErrPartialStatePath = errors.New("partial state manifest path")

// PartialStateFailure records one failed qualified table without credentials.
type PartialStateFailure struct {
	Table string `json:"table"`
	Error string `json:"error,omitempty"`
}

// PartialStateManifest records committed, failed, and pending qualified tables.
// It never stores DSNs, passwords, or other credentials.
type PartialStateManifest struct {
	Committed []string              `json:"committed"`
	Failed    []PartialStateFailure `json:"failed,omitempty"`
	Pending   []string              `json:"pending"`
}

// DefaultPartialStatePath returns the default manifest path under workDir.
func DefaultPartialStatePath(workDir string) string {
	return filepath.Join(workDir, defaultPartialState)
}

// ValidatePartialStatePath rejects empty, traversal, or directory manifest paths.
func ValidatePartialStatePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: empty path", ErrPartialStatePath)
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) && (clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))) {
		return fmt.Errorf("%w: path %q traverses parent directory", ErrPartialStatePath, path)
	}
	if clean == "." || clean == string(filepath.Separator) {
		return fmt.Errorf("%w: path %q is not a file", ErrPartialStatePath, path)
	}
	base := filepath.Base(clean)
	if base == "." || base == ".." {
		return fmt.Errorf("%w: path %q is not a file", ErrPartialStatePath, path)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: path %q is a symlink", ErrPartialStatePath, path)
		}
		if info.IsDir() {
			return fmt.Errorf("%w: path %q is a directory", ErrPartialStatePath, path)
		}
	}
	return nil
}

// mergePartialStateManifestForRetry carries forward committed tables from a prior
// partial manifest and re-pends every other table in the current restore scope.
func mergePartialStateManifestForRetry(existing PartialStateManifest, allLabels []string) PartialStateManifest {
	scope := make(map[string]struct{}, len(allLabels))
	for _, label := range allLabels {
		scope[label] = struct{}{}
	}
	committedSet := make(map[string]struct{}, len(existing.Committed))
	var retained []string
	for _, label := range existing.Committed {
		if _, ok := scope[label]; !ok {
			continue
		}
		if _, dup := committedSet[label]; dup {
			continue
		}
		committedSet[label] = struct{}{}
		retained = append(retained, label)
	}
	pending := make([]string, 0, len(allLabels))
	for _, label := range normalizeQualifiedList(allLabels) {
		if _, ok := committedSet[label]; !ok {
			pending = append(pending, label)
		}
	}
	return PartialStateManifest{
		Committed: normalizeQualifiedList(retained),
		Failed:    nil,
		Pending:   pending,
	}
}

// NewPartialStateManifest builds the initial pending-only manifest for tables.
func NewPartialStateManifest(tables []string) PartialStateManifest {
	normalized := normalizeQualifiedList(tables)
	return PartialStateManifest{
		Committed: nil,
		Failed:    nil,
		Pending:   normalized,
	}
}

// MarkCommitted moves one qualified table from pending to committed.
func (m *PartialStateManifest) MarkCommitted(table string) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("partial state: empty table name")
	}
	if !removeQualified(&m.Pending, table) {
		return fmt.Errorf("partial state: table %q is not pending", table)
	}
	m.Committed = appendQualified(m.Committed, table)
	normalizePartialStateManifest(m)
	return nil
}

// MarkFailed moves one qualified table from pending to failed with sanitized detail.
func (m *PartialStateManifest) MarkFailed(table string, err error) error {
	table = strings.TrimSpace(table)
	if table == "" {
		return fmt.Errorf("partial state: empty table name")
	}
	if !removeQualified(&m.Pending, table) {
		return fmt.Errorf("partial state: table %q is not pending", table)
	}
	detail := ""
	if err != nil {
		detail = sanitizeFailureDetail(err.Error())
	}
	m.Failed = append(m.Failed, PartialStateFailure{Table: table, Error: detail})
	normalizePartialStateManifest(m)
	return nil
}

// LoadPartialStateManifest reads a manifest from path.
func LoadPartialStateManifest(path string) (PartialStateManifest, error) {
	if err := ValidatePartialStatePath(path); err != nil {
		return PartialStateManifest{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PartialStateManifest{}, fmt.Errorf("read partial state manifest: %w", err)
	}
	var m PartialStateManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return PartialStateManifest{}, fmt.Errorf("decode partial state manifest: %w", err)
	}
	normalizePartialStateManifest(&m)
	return m, nil
}

// WritePartialStateManifest atomically persists manifest with 0600 permissions.
func WritePartialStateManifest(path string, m PartialStateManifest) error {
	if err := ValidatePartialStatePath(path); err != nil {
		return err
	}
	normalizePartialStateManifest(&m)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, partialStateDirMode); err != nil {
		return fmt.Errorf("create partial state directory: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal partial state manifest: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create partial state temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(partialStateFileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod partial state temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write partial state manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close partial state temp file: %w", err)
	}
	if err := partialStateRename(tmpPath, path); err != nil {
		return fmt.Errorf("replace partial state manifest: %w", err)
	}
	removeTemp = false
	return nil
}

var partialStateRename = os.Rename

// RemovePartialStateManifest deletes the manifest after successful restore.
func RemovePartialStateManifest(path string) error {
	if err := ValidatePartialStatePath(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove partial state manifest: %w", err)
	}
	return nil
}

func normalizePartialStateManifest(m *PartialStateManifest) {
	m.Committed = normalizeQualifiedList(m.Committed)
	m.Pending = normalizeQualifiedList(m.Pending)
	sort.Slice(m.Failed, func(i, j int) bool {
		if m.Failed[i].Table == m.Failed[j].Table {
			return m.Failed[i].Error < m.Failed[j].Error
		}
		return m.Failed[i].Table < m.Failed[j].Table
	})
	seenFailed := make(map[string]struct{}, len(m.Failed))
	filtered := m.Failed[:0]
	for _, f := range m.Failed {
		if f.Table == "" {
			continue
		}
		if _, ok := seenFailed[f.Table]; ok {
			continue
		}
		seenFailed[f.Table] = struct{}{}
		filtered = append(filtered, PartialStateFailure{
			Table: f.Table,
			Error: sanitizeFailureDetail(f.Error),
		})
	}
	m.Failed = filtered
}

func normalizeQualifiedList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func appendQualified(values []string, table string) []string {
	for _, existing := range values {
		if existing == table {
			return values
		}
	}
	return append(values, table)
}

func removeQualified(values *[]string, table string) bool {
	for i, existing := range *values {
		if existing == table {
			*values = append((*values)[:i], (*values)[i+1:]...)
			return true
		}
	}
	return false
}

func sanitizeFailureDetail(msg string) string {
	msg = strings.TrimSpace(connections.RedactMessage(msg))
	if len(msg) > maxFailureDetailLen {
		msg = msg[:maxFailureDetailLen] + "..."
	}
	return msg
}
