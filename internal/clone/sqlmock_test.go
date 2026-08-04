package clone

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// pgTextArrayConverter lets sqlmock match []string scope arguments passed to pgx-style queries.
type pgTextArrayConverter struct{}

func (pgTextArrayConverter) ConvertValue(v interface{}) (driver.Value, error) {
	if s, ok := v.([]string); ok {
		return s, nil
	}
	return driver.DefaultParameterConverter.ConvertValue(v)
}

func newSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(
		sqlmock.MonitorPingsOption(true),
		sqlmock.ValueConverterOption(pgTextArrayConverter{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}
