package tui

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/schemacapture"
)

// dumpMetadataTables reads table stats from metadata.json for result summary.
func dumpMetadataTables(dir string) (tables []DumpTableStat, ok bool) {
	meta, err := dump.ReadMetadata(dir)
	if err != nil {
		return nil, false
	}
	out := make([]DumpTableStat, 0, len(meta.Tables))
	for _, tbl := range meta.Tables {
		out = append(out, DumpTableStat{
			Name:        tbl.Name,
			RowEstimate: tbl.RowCount,
		})
	}
	return out, true
}

type dumpRequestedMsg struct{}

type dumpProgressMsg struct {
	line string
	ev   *DumpProgressEvent
}

type dumpResultMsg struct {
	err error
}

// DumpRunner runs a dump against the connected session.
type DumpRunner interface {
	Run(ctx context.Context, db *sql.DB, outputDir string, draft DumpDraft, schemas []string, sourceDB, sourceDSN string, onProgress func(dump.ProgressEvent)) error
}

type productionDumpRunner struct{}

func (productionDumpRunner) Run(ctx context.Context, db *sql.DB, outputDir string, draft DumpDraft, schemas []string, sourceDB, sourceDSN string, onProgress func(dump.ProgressEvent)) error {
	effectiveSchemas := append([]string(nil), schemas...)
	if len(effectiveSchemas) == 0 {
		effectiveSchemas = []string{"public"}
	}
	var opts []dump.Option
	if draft.NoTransaction {
		opts = append(opts, dump.WithoutTransaction())
	}
	opts = append(opts, dump.WithSchemas(effectiveSchemas))
	if onProgress != nil {
		opts = append(opts, dump.WithProgress(onProgress))
	}
	if cfg, err := config.LoadConfig(config.ResolveConfigPath()); err == nil {
		opts = append(opts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	}
	if seq, ok := parseDumpSeq(outputDir); ok {
		opts = append(opts, dump.WithProvenance(dump.Provenance{
			Seq:            seq,
			BaseDir:        draft.OutputDir,
			SourceDatabase: sourceDB,
			Schemas:        append([]string(nil), effectiveSchemas...),
		}))
	}
	if err := dump.Dump(ctx, db, outputDir, opts...); err != nil {
		return err
	}
	if sourceDSN != "" {
		if err := schemacapture.Capture(ctx, sourceDSN, outputDir, effectiveSchemas); err != nil && onProgress != nil {
			onProgress(dump.ProgressEvent{Phase: "schema_capture_warning", Table: err.Error()})
		}
	}
	return nil
}

func parseDumpSeq(outputDir string) (int, bool) {
	base := filepath.Base(outputDir)
	n, err := strconv.Atoi(base)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func formatDumpProgress(ev dump.ProgressEvent) string {
	switch ev.Phase {
	case "table_start":
		return fmt.Sprintf("dumping %s…", ev.Table)
	case "table_end":
		return fmt.Sprintf("done %s", ev.Table)
	default:
		return fmt.Sprintf("%s %s", ev.Phase, ev.Table)
	}
}

func waitDumpCmd(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return dumpResultMsg{err: fmt.Errorf("dump channel closed")}
		}
		return msg
	}
}

func startDumpCmd(runner DumpRunner, ctx context.Context, db *sql.DB, outputDir string, draft DumpDraft, schemas []string, sourceDB, sourceDSN string) (tea.Cmd, <-chan tea.Msg, context.CancelFunc) {
	ch := make(chan tea.Msg, 32)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(ch)
		onProgress := func(ev dump.ProgressEvent) {
			line := formatDumpProgress(ev)
			localEv := &DumpProgressEvent{
				Phase:   ev.Phase,
				Table:   ev.Table,
				Current: ev.Current,
				Total:   ev.Total,
				Elapsed: ev.Elapsed,
			}
			select {
			case ch <- dumpProgressMsg{line: line, ev: localEv}:
			case <-ctx.Done():
			}
		}
		err := runner.Run(ctx, db, outputDir, draft, schemas, sourceDB, sourceDSN, onProgress)
		ch <- dumpResultMsg{err: err}
	}()
	return waitDumpCmd(ch), ch, cancel
}
