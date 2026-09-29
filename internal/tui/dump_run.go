package tui

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/VicenteOlmos/dolly/internal/config"
	"github.com/VicenteOlmos/dolly/internal/dump"
	"github.com/VicenteOlmos/dolly/internal/runopts"
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

	cfg, err := config.LoadConfig(config.ResolveConfigPath())
	if err != nil {
		return err
	}

	overrides, err := dumpOverridesFromDraft(draft)
	if err != nil {
		return err
	}
	opts, err := runopts.BuildDumpOptions(overrides, cfg)
	if err != nil {
		return err
	}
	if err := dump.ValidateWorkerPoolHeadroom(dump.InspectWorkers(opts...), workerPoolHeadroom(cfg)); err != nil {
		return err
	}

	opts = append(opts, dump.WithSchemas(effectiveSchemas))
	if onProgress != nil {
		opts = append(opts, dump.WithProgress(onProgress))
	}
	opts = append(opts, dump.SanitizationOptions(cfg.Sanitization.Enabled)...)
	sanitized := cfg.Sanitization.Enabled
	if seq, ok := parseDumpSeq(outputDir); ok {
		opts = append(opts, dump.WithProvenance(dump.Provenance{
			Seq:            seq,
			BaseDir:        draft.OutputDir,
			SourceDatabase: sourceDB,
			Schemas:        append([]string(nil), effectiveSchemas...),
			Sanitized:      &sanitized,
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

func dumpOverridesFromDraft(draft DumpDraft) (runopts.DumpOverrides, error) {
	percent, err := parseDraftPercent(draft.PercentText)
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	maxDepth, err := parseOptionalNonNegative(draft.MaxDepthText, "max depth")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	maxTables, err := parseOptionalNonNegative(draft.MaxTablesText, "max tables")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	maxRows, err := parseOptionalNonNegative(draft.MaxRowsText, "max rows")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	maxRowsPer, err := parseOptionalNonNegative(draft.MaxRowsPerTableText, "max rows per table")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	chunkSize, err := parseOptionalPositiveInt(draft.ChunkSizeText, "chunk size")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	retryMax, err := parseOptionalNonNegative(draft.RetryMaxText, "retry max")
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	retryBase, err := parseDraftRetryBase(draft.RetryBaseText)
	if err != nil {
		return runopts.DumpOverrides{}, err
	}
	return runopts.DumpOverrides{
		NoTransaction:   draft.NoTransaction,
		SlowConnection:  draft.SlowConnection,
		RequireSafeKey:  draft.RequireSafeKey,
		Percent:         percent,
		SeedFile:        strings.TrimSpace(draft.SeedFile),
		ChunkTables:     splitChunkTables(draft.ChunkTables),
		MaxDepth:        maxDepth,
		MaxTables:       maxTables,
		MaxRows:         maxRows,
		MaxRowsPerTable: maxRowsPer,
		IncludeTables:   splitChunkTables(draft.IncludeTables),
		ExcludeTables:   splitChunkTables(draft.ExcludeTables),
		ChunkSize:       chunkSize,
		RetryMax:        retryMax,
		RetryMaxSet:     strings.TrimSpace(draft.RetryMaxText) != "",
		RetryBase:       retryBase,
		Workers:         draft.Workers,
		WorkersSet:      draft.WorkersSet,
	}, nil
}

func parseDraftRetryBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return "", fmt.Errorf("retry base must be a duration: %w", err)
	}
	if d <= 0 {
		return "", fmt.Errorf("retry base must be positive")
	}
	return raw, nil
}

func parseOptionalPositiveInt(raw, label string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be an integer >= 1", label)
	}
	return n, nil
}

func parseOptionalNonNegative(raw, label string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", label)
	}
	return n, nil
}

func parseDraftPercent(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		return 0, fmt.Errorf("percent must be an integer from 1 to 100")
	}
	return n, nil
}

func splitChunkTables(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func maxDumpWorkers() int {
	return dump.MaxParallelWorkers()
}

func workerPoolHeadroom(cfg *config.Config) int {
	maxConns := cfg.DB.MaxOpenConns
	if maxConns <= 0 {
		maxConns = 5
	}
	return maxConns
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
			return nil
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
			sendProgress(ctx, ch, dumpProgressMsg{line: line, ev: localEv})
		}
		err := runner.Run(ctx, db, outputDir, draft, schemas, sourceDB, sourceDSN, onProgress)
		deliverResult(ctx, ch, dumpResultMsg{err: err})
	}()
	return waitDumpCmd(ch), ch, cancel
}
