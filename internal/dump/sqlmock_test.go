package dump

import "github.com/DATA-DOG/go-sqlmock"

func emptyUniqueIndexMock(mock sqlmock.Sqlmock) {
	idxRows := sqlmock.NewRows([]string{
		"nspname", "relname", "index_name", "index_oid", "indisprimary",
		"indisvalid", "indisready", "amname", "has_predicate",
		"is_expression", "indnkeyatts", "attname", "is_nullable", "pos",
		"attnum", "opclass_oid", "collation_oid", "optval",
	})
	mock.ExpectQuery(`pg_index[\s\S]*indisunique`).WillReturnRows(idxRows)
}
