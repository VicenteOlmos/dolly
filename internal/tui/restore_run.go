package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/restore"
	"github.com/VicenteOlmos/dolly/internal/runopts"
)

type restoreConfirmRequestedMsg struct {
	inputDir         string
	trustedSchemaSQL bool
}

type restoreRequestedMsg struct {
	inputDir         string
	trustedSchemaSQL bool
}

type restoreProgressMsg struct {
	line string
	ev   RestoreProgressEvent
}

type restoreResultMsg struct {
	err error
}

// RestoreRunner restores a dump directory into the connected session.
type RestoreRunner interface {
	Run(ctx context.Context, db *sql.DB, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, onProgress func(restore.ProgressEvent)) error
}

type productionRestoreRunner struct{}

// restoreHistoryOverrides carries optional TUI history restore settings for one run.
type restoreHistoryOverrides struct {
	OnConflict  string
	Replace     bool
	ReplaceSet  bool
	WorkersText string
}

func restoreHistoryUserOverrides(history restoreHistoryOverrides) (runopts.RestoreHistoryUserOverrides, error) {
	o := runopts.RestoreHistoryUserOverrides{
		OnConflict: history.OnConflict,
		Replace:    history.Replace,
		ReplaceSet: history.ReplaceSet,
	}
	raw := strings.TrimSpace(history.WorkersText)
	if raw == "" {
		return o, nil
	}
	workers, err := strconv.Atoi(raw)
	if err != nil {
		return runopts.RestoreHistoryUserOverrides{}, fmt.Errorf("restore workers must be an integer between 1 and %d", restore.MaxParallelRestoreWorkers())
	}
	if err := runopts.ValidateRestoreWorkers(workers); err != nil {
		return runopts.RestoreHistoryUserOverrides{}, err
	}
	o.Workers = workers
	o.WorkersSet = true
	return o, nil
}

func effectiveRestoreWorkers(cfg *config.Config, history restoreHistoryOverrides) (int, error) {
	o, err := restoreHistoryUserOverrides(history)
	if err != nil {
		return 0, err
	}
	if o.WorkersSet {
		return o.Workers, nil
	}
	if cfg == nil {
		return 1, nil
	}
	return runopts.ResolveRestoreWorkers(runopts.RestoreOverrides{}, cfg), nil
}

func restoreHistoryOverridesFromDraft(d DumpDraft) restoreHistoryOverrides {
	return restoreHistoryOverrides{
		OnConflict:  d.RestoreOnConflict,
		Replace:     d.RestoreReplace,
		ReplaceSet:  d.RestoreReplaceSet,
		WorkersText: d.RestoreWorkersText,
	}
}

func (productionRestoreRunner) Run(ctx context.Context, db *sql.DB, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, onProgress func(restore.ProgressEvent)) error {
	return runProductionRestoreHistory(ctx, db, inputDir, schemas, trustedSchemaSQL, dsn, onProgress, restoreHistoryOverrides{})
}

func runProductionRestoreHistory(ctx context.Context, db *sql.DB, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, onProgress func(restore.ProgressEvent), history restoreHistoryOverrides) error {
	cfg, err := config.LoadConfig(config.ResolveConfigPath())
	if err != nil {
		return err
	}
	userOverrides, err := restoreHistoryUserOverrides(history)
	if err != nil {
		return err
	}
	opts, err := runopts.RestoreHistoryOptionsWithOverrides(cfg, inputDir, schemas, trustedSchemaSQL, dsn, userOverrides)
	if err != nil {
		return err
	}
	if onProgress != nil {
		opts = append(opts, restore.WithProgress(onProgress))
	}
	return restore.Restore(ctx, db, inputDir, opts...)
}

func invokeRestoreRunner(runner RestoreRunner, ctx context.Context, db *sql.DB, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, onProgress func(restore.ProgressEvent), history restoreHistoryOverrides) error {
	if _, ok := runner.(productionRestoreRunner); ok {
		return runProductionRestoreHistory(ctx, db, inputDir, schemas, trustedSchemaSQL, dsn, onProgress, history)
	}
	return runner.Run(ctx, db, inputDir, schemas, trustedSchemaSQL, dsn, onProgress)
}

func formatRestoreProgress(ev restore.ProgressEvent) string {
	switch ev.Phase {
	case "table_start":
		return fmt.Sprintf("restoring %s…", ev.Table)
	case "table_end":
		return fmt.Sprintf("done %s", ev.Table)
	default:
		return fmt.Sprintf("%s %s", ev.Phase, ev.Table)
	}
}

func startRestoreCmd(runner RestoreRunner, ctx context.Context, db *sql.DB, inputDir string, schemas []string, trustedSchemaSQL bool, dsn string, history restoreHistoryOverrides) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
	ch := make(chan tea.Msg, 32)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(ch)
		onProgress := func(ev restore.ProgressEvent) {
			line := formatRestoreProgress(ev)
			localEv := RestoreProgressEvent{
				Phase:   ev.Phase,
				Table:   ev.Table,
				Current: ev.Current,
				Total:   ev.Total,
				Elapsed: ev.Elapsed,
			}
			sendProgress(ctx, ch, restoreProgressMsg{line: line, ev: localEv})
		}
		err := invokeRestoreRunner(runner, ctx, db, inputDir, schemas, trustedSchemaSQL, dsn, onProgress, history)
		deliverResult(ctx, ch, restoreResultMsg{err: err})
	}()
	return waitRestoreCmd(ch), ch, cancel
}

func waitRestoreCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
