package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is the PostgreSQL error code for a broken UNIQUE constraint.
const uniqueViolation = "23505"

// isUniqueViolation reports whether err is PostgreSQL refusing a row that
// would break a UNIQUE constraint.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
