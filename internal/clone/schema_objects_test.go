package clone

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRefreshMaterializedViewIfPopulatedDuringReplay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	populated := viewRow{schema: "app", name: "mv", materialized: true, populated: true}
	mock.ExpectExec(`REFRESH MATERIALIZED VIEW "app"."mv"`).WillReturnResult(sqlmock.NewResult(0, 0))
	if err := refreshMaterializedViewIfPopulated(context.Background(), db, populated); err != nil {
		t.Fatal(err)
	}

	unpopulated := viewRow{schema: "app", name: "empty_mv", materialized: true, populated: false}
	if err := refreshMaterializedViewIfPopulated(context.Background(), db, unpopulated); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
