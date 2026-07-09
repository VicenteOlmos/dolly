package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/clonework"
	"github.com/VicenteOlmos/dolly/internal/dbanalyze"
)

type cloneRequestedMsg struct{}

type cloneProceedMsg struct{}

// CloneProgressEvent mirrors clone.ProgressEvent without importing the clone package.
type CloneProgressEvent struct {
	Phase   string
	Step    string
	Table   string
	Current int
	Total   int
	Elapsed time.Duration
}

type cloneProgressMsg struct {
	line string
	ev   CloneProgressEvent
}

type cloneResultMsg struct {
	err error
}

type analyzeRequestedMsg struct{}

type analyzeResultMsg struct {
	result analyzeResult
	err    error
}

// analyzeResult is the TUI alias for the shared analyze preflight result type.
type analyzeResult = dbanalyze.AnalyzeResult

// ObjectStat is the TUI alias for per-object analyze stats.
type ObjectStat = dbanalyze.ObjectStat

// analyzeSourceFunc is the test seam for the analyze preflight.
var analyzeSourceFunc = dbanalyze.AnalyzeSource

// cloneName replaces "{db}" and initial "{n}" in template for TUI prefill.
// This is a local copy of clone.CloneName plus default n=1 to avoid importing
// the forbidden clone package for clone execution.
func cloneName(sourceDB, template string) string {
	if template == "" {
		template = "{db}_dolly_{n}"
	}
	name := strings.ReplaceAll(template, "{db}", sourceDB)
	return strings.ReplaceAll(name, "{n}", "1")
}

// startAnalyzeCmd runs the analyze preflight asynchronously.
func startAnalyzeCmd(sqlDB *sql.DB, sourceDB, nameTpl string, schemas []string) (tea.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := func() tea.Msg {
		result, err := analyzeSourceFunc(ctx, sqlDB, sourceDB, nameTpl, schemas)
		if err != nil {
			return analyzeResultMsg{err: err}
		}
		return analyzeResultMsg{result: result}
	}
	return cmd, cancel
}

// CloneRunner runs clone against the connected source session.
type CloneRunner interface {
	Run(ctx context.Context, draft CloneDraft, schemas []string, onProgress func(CloneProgressEvent)) error
}

type productionCloneRunner struct{}

func (productionCloneRunner) Run(ctx context.Context, draft CloneDraft, schemas []string, onProgress func(CloneProgressEvent)) error {
	// Adapter: convert local CloneProgressEvent to clonework.ProgressEvent (which is clone.ProgressEvent).
	var wrapped func(clonework.ProgressEvent)
	if onProgress != nil {
		wrapped = func(ev clonework.ProgressEvent) {
			onProgress(CloneProgressEvent{
				Phase:   ev.Phase,
				Step:    ev.Step,
				Table:   ev.Table,
				Current: ev.Current,
				Total:   ev.Total,
				Elapsed: ev.Elapsed,
			})
		}
	}
	return clonework.Run(ctx, clonework.Params{
		SourceDSN: draft.SourceDSN,
		CloneName: draft.CloneName,
		TargetDSN: draft.TargetDSN,
		Strategy:  draft.Strategy,
		Schemas:   schemas,
	}, wrapped)
}

func waitCloneCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return cloneResultMsg{err: fmt.Errorf("clone channel closed")}
		}
		return msg
	}
}

func startCloneCmd(runner CloneRunner, ctx context.Context, draft CloneDraft, schemas []string) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
	ch := make(chan tea.Msg, 32)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(ch)
		onProgress := func(ev CloneProgressEvent) {
			line := formatCloneProgress(ev)
			select {
			case ch <- cloneProgressMsg{line: line, ev: ev}:
			case <-ctx.Done():
			}
		}
		err := runner.Run(ctx, draft, schemas, onProgress)
		ch <- cloneResultMsg{err: err}
	}()
	return waitCloneCmd(ch), ch, cancel
}

func formatCloneProgress(ev CloneProgressEvent) string {
	if ev.Step != "" {
		return ev.Step
	}
	return fmt.Sprintf("%s %s", ev.Phase, ev.Table)
}

func appendCloneLog(log *[]string, line string) {
	*log = append(*log, line)
	if len(*log) > dumpLogMaxLines {
		*log = (*log)[len(*log)-dumpLogMaxLines:]
	}
}
