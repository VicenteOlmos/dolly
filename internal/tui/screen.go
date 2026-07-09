package tui

import (
	"context"
	"net"
	"net/url"

	tea "charm.land/bubbletea/v2"
)

type Screen int

const (
	ScreenConnection Screen = iota
	ScreenSchema
	ScreenDump
	ScreenClone
	ScreenConfig
)

func (s Screen) Name() string {
	switch s {
	case ScreenConnection:
		return "connection"
	case ScreenSchema:
		return "schema"
	case ScreenDump:
		return "dump"
	case ScreenClone:
		return "clone"
	case ScreenConfig:
		return "config"
	default:
		return "unknown"
	}
}

const (
	minTerminalWidth  = 60
	minTerminalHeight = 20
)

type ConnectionStatus int

const (
	ConnStatusIdle ConnectionStatus = iota
	ConnStatusConnecting
	ConnStatusConnected
	ConnStatusError
)

type DumpStatus int

const (
	DumpStatusIdle DumpStatus = iota
	DumpStatusRunning
	DumpStatusError
	DumpStatusComplete
)

type DumpOutcome int

const (
	DumpOutcomeSuccess DumpOutcome = iota
	DumpOutcomeError
)

type DumpTableStat struct {
	Name        string
	RowEstimate *int64
}

type DumpResultSummary struct {
	Outcome          DumpOutcome
	OutputDir        string
	Error            string
	Files            []string
	HasIncomplete    bool
	TableCount       int
	Tables           []DumpTableStat
	TotalRowEstimate *int64
	MetadataMissing  bool
}

type ConnectionDraft struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
	SSLMODE  string
}

func (c ConnectionDraft) DSN() string {
	port := c.Port
	if port == "" {
		port = "5432"
	}
	sslmode := c.SSLMODE
	if sslmode == "" {
		sslmode = "verify-full"
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, port),
		Path:   "/" + c.Database,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	if sslmode != "disable" {
		q.Set("channel_binding", "require")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

type SchemaDraft struct {
	Tables            []string
	TableCount        int
	ColumnCount       int
	FKCount           int
	TableScrollOffset int
}

type DumpDraft struct {
	OutputDir     string
	NoTransaction bool
	SchemaPicker  SchemaPickerState
	History       DumpHistoryState
}

// DumpHistoryEntry is one row in the dump history list.
type DumpHistoryEntry struct {
	Seq         int
	Path        string
	Label       string // e.g. "#3 · public · 12 tables"
	Schemas     []string
	TableCount  int
	RowEstimate int64
}

type DumpHistoryState struct {
	Entries []DumpHistoryEntry
	Cursor  int
}

func (h *DumpHistoryState) Selected() *DumpHistoryEntry {
	if h == nil || h.Cursor < 0 || h.Cursor >= len(h.Entries) {
		return nil
	}
	return &h.Entries[h.Cursor]
}

func (h *DumpHistoryState) MoveCursor(delta int) {
	if len(h.Entries) == 0 {
		h.Cursor = 0
		return
	}
	h.Cursor += delta
	if h.Cursor < 0 {
		h.Cursor = 0
	}
	if h.Cursor >= len(h.Entries) {
		h.Cursor = len(h.Entries) - 1
	}
}

type CloneStatus int

const (
	CloneStatusIdle CloneStatus = iota
	CloneStatusRunning
	CloneStatusError
	CloneStatusComplete
)

type CloneDraft struct {
	SourceDSN         string
	CloneName         string
	TargetDSN         string
	Strategy          string
	SchemaPicker      SchemaPickerState
	TargetSource      TargetSource
	TargetProfileName string
	AnalyzeEnabled    bool
	AnalyzeState      AnalyzeState
}

// TargetSource selects where the clone target DSN comes from.
type TargetSource int

const (
	TargetSourceCurrent TargetSource = iota // use the current connection DSN
	TargetSourceSaved                       // use a saved connection profile
	TargetSourceManual                      // user-typed DSN
)

func (ts TargetSource) String() string {
	switch ts {
	case TargetSourceCurrent:
		return "Current DSN"
	case TargetSourceSaved:
		return "Saved"
	case TargetSourceManual:
		return "Manual"
	default:
		return "unknown"
	}
}

// AnalyzeState tracks the async analyze preflight.
type AnalyzeState struct {
	Loading bool
	Result  *analyzeResult
	Err     string
	Cancel  context.CancelFunc
}

type ScreenModel interface {
	Update(msg tea.Msg) tea.Cmd
	View(width, height int) string
}
