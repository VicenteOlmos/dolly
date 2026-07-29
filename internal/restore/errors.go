package restore

import (
	"errors"
	"fmt"
)

// ErrEmptyDump marks restore attempts against dumps with zero tables.
var ErrEmptyDump = errors.New("empty dump")

// EmptyDumpError reports a dump directory that contains no table metadata.
type EmptyDumpError struct {
	InputDir string
}

func (e *EmptyDumpError) Error() string {
	return "dump contains no tables"
}

func (e *EmptyDumpError) Is(target error) bool {
	return target == ErrEmptyDump
}

// IsEmptyDumpError reports whether err is a zero-table restore refusal.
func IsEmptyDumpError(err error) bool {
	var empty *EmptyDumpError
	return errors.As(err, &empty)
}

// ErrNoDataFiles marks restore attempts against dumps with table metadata but no data files.
var ErrNoDataFiles = errors.New("dump has no data files")

// NoDataFilesError reports a dump directory whose metadata lists tables but contains no NDJSON data.
type NoDataFilesError struct {
	InputDir   string
	TableCount int
}

func (e *NoDataFilesError) Error() string {
	return fmt.Sprintf("dump contains metadata for %d table(s) but no data files", e.TableCount)
}

func (e *NoDataFilesError) Is(target error) bool {
	return target == ErrNoDataFiles
}

// IsNoDataFilesError reports whether err is a zero-data-file restore refusal.
func IsNoDataFilesError(err error) bool {
	var noData *NoDataFilesError
	return errors.As(err, &noData)
}
