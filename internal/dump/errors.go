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
