package clone

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestApplyStatisticsExecutesDefinition(t *testing.T) {
	tgt, tgtMock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tgt.Close() })

	def := `CREATE STATISTICS "app"."users_stats" ON id, name FROM "app"."users"`
	tgtMock.ExpectExec(regexp.QuoteMeta(def)).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := applyStatistics(context.Background(), tgt, []string{def}); err != nil {
		t.Fatal(err)
	}
	if err := tgtMock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
