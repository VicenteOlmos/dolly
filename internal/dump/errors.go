package dump

import (
	"errors"
	"fmt"
	"strings"

	"github.com/VicenteOlmos/dolly/internal/db"
)

// ErrNoTables marks dumps that discovered or selected zero tables.
var ErrNoTables = errors.New("no tables to dump")

// NoTablesError reports schema scope with no dumpable tables after discovery/selection.
type NoTablesError struct {
	Schemas []string
}

func (e *NoTablesError) Error() string {
	schemas := e.Schemas
	if len(schemas) == 0 {
		schemas = []string{"public"}
	}
	return fmt.Sprintf("no tables found in schema(s) %s", strings.Join(schemas, ", "))
}

func (e *NoTablesError) Is(target error) bool {
	return target == ErrNoTables
}

// IsNoTablesError reports whether err is a zero-table dump refusal.
func IsNoTablesError(err error) bool {
	var noTables *NoTablesError
	return errors.As(err, &noTables)
}

func guardSelectedTables(tables []db.Table, schemas []string) error {
	if len(tables) > 0 {
		return nil
	}
	effective := schemas
	if len(effective) == 0 {
		effective = []string{"public"}
	}
	return &NoTablesError{Schemas: append([]string(nil), effective...)}
}

// ErrSequenceScopeOutofRange marks sequence capture failures where a returned
// sequence belongs to a schema outside the configured effective schemas.
var ErrSequenceScopeOutofRange = errors.New("sequence scope out of range")

// SequenceScopeOutofRangeError reports a sequence whose schema is not in the
// requested scope. The dump exits nonzero before publishing any metadata or
// schema artifacts.
type SequenceScopeOutofRangeError struct {
	Schema  string
	SeqName string
}

func (e *SequenceScopeOutofRangeError) Error() string {
	return fmt.Sprintf("sequence %s.%s: schema %q is not in the requested scope", e.Schema, e.SeqName, e.Schema)
}

func (e *SequenceScopeOutofRangeError) Is(target error) bool {
	return target == ErrSequenceScopeOutofRange
}

// IsSequenceScopeOutofRangeError reports whether err is a scope-out-of-range
// sequence capture failure.
func IsSequenceScopeOutofRangeError(err error) bool {
	var scopeErr *SequenceScopeOutofRangeError
	return errors.As(err, &scopeErr)
}

// guardSequenceScope validates that every captured sequence belongs to one of
// the effective schemas. When schemas is empty (no explicit filter), all
// sequences pass. When schemas is non-empty, it returns
// *SequenceScopeOutofRangeError on the first out-of-scope sequence found.
func guardSequenceScope(seqs []SequenceState, schemas []string) error {
	if len(schemas) == 0 {
		return nil
	}
	scopeSet := make(map[string]struct{}, len(schemas))
	for _, s := range schemas {
		scopeSet[s] = struct{}{}
	}
	for _, seq := range seqs {
		if _, ok := scopeSet[seq.Schema]; !ok {
			return &SequenceScopeOutofRangeError{Schema: seq.Schema, SeqName: seq.Name}
		}
	}
	return nil
}
