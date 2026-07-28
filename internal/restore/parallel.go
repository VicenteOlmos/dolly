package restore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VicenteOlmos/dolly/internal/db"
	"github.com/VicenteOlmos/dolly/internal/dump"
)

const maxParallelRestoreWorkers = 16

// MaxParallelRestoreWorkers returns the upper bound for parallel restore worker count.
func MaxParallelRestoreWorkers() int {
	return maxParallelRestoreWorkers
}

// WithWorkers sets parallel restore worker count. Values <=0 default to 1; values above
// maxParallelRestoreWorkers are rejected when parallel restore runs.
func WithWorkers(n int) Option {
	return func(c *config) {
		c.workers = n
	}
}

// WithPartialStateManifest sets the durable partial-state manifest path for parallel restore.
func WithPartialStateManifest(path string) Option {
	return func(c *config) {
		c.partialStatePath = path
	}
}

func effectiveRestoreWorkers(workers int) int {
	if workers <= 0 {
		return 1
	}
	return workers
}

func validateParallelRestoreOptions(cfg *config) error {
	workers := effectiveRestoreWorkers(cfg.workers)
	if workers <= 1 {
		return nil
	}
	if workers > maxParallelRestoreWorkers {
		return fmt.Errorf("parallel restore workers must be between 1 and %d", maxParallelRestoreWorkers)
	}
	if !cfg.withoutTransaction {
		return fmt.Errorf("parallel restore requires WithoutTransaction")
	}
	if strings.TrimSpace(cfg.dsn) == "" {
		return fmt.Errorf("parallel restore requires DSN for COPY")
	}
	if cfg.replace {
		return fmt.Errorf("parallel restore is incompatible with replace")
	}
	if cfg.schemaSQL {
		return fmt.Errorf("parallel restore is incompatible with trusted schema replay")
	}
	policy := cfg.policy
	if !canUseCopy(policy) {
		return fmt.Errorf("parallel restore requires conflict policy error (COPY incompatible with %s)", policy)
	}
	if strings.TrimSpace(cfg.partialStatePath) == "" {
		return fmt.Errorf("parallel restore requires partial state manifest path")
	}
	return ValidatePartialStatePath(cfg.partialStatePath)
}

type parallelTableJob struct {
	label string
	table db.Table
	path  string
}

var (
	parallelLoadTableCopy    = loadTableCopy
	parallelRestoreSequences = RestoreSequencesFromMetadata
	parallelSyncSequences    = SyncSequencesToData
	parallelWriteManifest    = WritePartialStateManifest
	parallelRemoveManifest   = RemovePartialStateManifest
)

func buildParallelTableMaps(tables []db.Table, dataPaths []string) (map[string]db.Table, map[string]string, []string) {
	byLabel := make(map[string]db.Table, len(tables))
	paths := make(map[string]string, len(tables))
	labels := make([]string, 0, len(tables))
	for i, table := range tables {
		label := qualifiedLabel(table.Schema, table.Name)
		byLabel[label] = table
		paths[label] = dataPaths[i]
		labels = append(labels, label)
	}
	return byLabel, paths, labels
}

func runParallelRestore(
	ctx context.Context,
	cfg *config,
	dbConn *sql.DB,
	meta dump.Metadata,
	dataPaths []string,
	levels []RestoreLevel,
	schemaFilter []string,
	workers int,
	startedAt time.Time,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	byLabel, pathsByLabel, allLabels := buildParallelTableMaps(meta.Tables, dataPaths)
	manifest := NewPartialStateManifest(allLabels)
	if err := parallelWriteManifest(cfg.partialStatePath, manifest); err != nil {
		return fmt.Errorf("write initial partial state: %w", err)
	}

	totalTables := len(allLabels)
	var manifestMu sync.Mutex
	var progressMu sync.Mutex
	var completed atomic.Int32
	var firstErr error
	var firstErrOnce sync.Once

	recordErr := func(err error) {
		if err == nil {
			return
		}
		firstErrOnce.Do(func() {
			firstErr = err
			cancel()
		})
	}

	recordTableFailure := func(label string, copyErr error) {
		firstErrOnce.Do(func() {
			cancel()
			manifestMu.Lock()
			var persistErr error
			if err := manifest.MarkFailed(label, copyErr); err != nil {
				persistErr = fmt.Errorf("record failed table %s: %w", label, err)
			} else if err := parallelWriteManifest(cfg.partialStatePath, manifest); err != nil {
				persistErr = fmt.Errorf("persist partial state: %w", err)
			}
			manifestMu.Unlock()
			if persistErr != nil {
				firstErr = errors.Join(copyErr, persistErr)
			} else {
				firstErr = copyErr
			}
		})
	}

	emit := func(ev ProgressEvent) {
		progressMu.Lock()
		emitProgress(cfg, ev)
		progressMu.Unlock()
	}

	markCommitted := func(label string) error {
		manifestMu.Lock()
		defer manifestMu.Unlock()
		if err := manifest.MarkCommitted(label); err != nil {
			return err
		}
		return parallelWriteManifest(cfg.partialStatePath, manifest)
	}

	for _, level := range levels {
		if ctx.Err() != nil {
			break
		}

		jobs := make(chan parallelTableJob, len(level.Tables))
		var wg sync.WaitGroup

		levelWorkers := workers
		if len(level.Tables) < levelWorkers {
			levelWorkers = len(level.Tables)
		}

		for workerID := 1; workerID <= levelWorkers; workerID++ {
			wg.Add(1)
			go func(workerNum int) {
				defer wg.Done()
				for job := range jobs {
					if ctx.Err() != nil {
						return
					}
					done := int(completed.Load())
					emit(ProgressEvent{
						Phase:   "table_start",
						Table:   job.label,
						Worker:  workerNum,
						Current: done + 1,
						Total:   totalTables,
						Elapsed: time.Since(startedAt),
					})
					if err := parallelLoadTableCopy(ctx, cfg.dsn, job.table, job.path); err != nil {
						recordTableFailure(job.label, err)
						return
					}
					if err := markCommitted(job.label); err != nil {
						recordErr(fmt.Errorf("update partial state for %s: %w", job.label, err))
						return
					}
					n := completed.Add(1)
					emit(ProgressEvent{
						Phase:   "table_end",
						Table:   job.label,
						Worker:  workerNum,
						Current: int(n),
						Total:   totalTables,
						Elapsed: time.Since(startedAt),
					})
				}
			}(workerID)
		}

		go func(tables []string) {
			for _, label := range tables {
				table := byLabel[label]
				select {
				case <-ctx.Done():
					close(jobs)
					return
				case jobs <- parallelTableJob{label: label, table: table, path: pathsByLabel[label]}:
				}
			}
			close(jobs)
		}(level.Tables)

		wg.Wait()
		if firstErr != nil {
			return firstErr
		}
	}

	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	seqQ := execQuerier(dbConn)
	if err := parallelRestoreSequences(ctx, seqQ, meta, schemaFilter); err != nil {
		return fmt.Errorf("restore sequences: %w", err)
	}
	if err := parallelSyncSequences(ctx, seqQ, schemaFilter); err != nil {
		return fmt.Errorf("sync sequences to data: %w", err)
	}
	if err := parallelRemoveManifest(cfg.partialStatePath); err != nil {
		return fmt.Errorf("remove partial state manifest: %w", err)
	}
	return nil
}
