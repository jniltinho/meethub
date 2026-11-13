package database

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
)

// Portable constraint errors, returned by Translate.
var (
	// ErrDuplicate is a unique or primary-key violation.
	ErrDuplicate = errors.New("duplicate value")
	// ErrForeignKey is a foreign-key violation.
	ErrForeignKey = errors.New("foreign key violation")
)

// SQLite extended result codes (https://sqlite.org/rescode.html).
const (
	sqliteConstraintForeignKey = 787
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

// Translate maps driver constraint errors of any dialect to ErrDuplicate or
// ErrForeignKey (wrapping the original); other errors are returned unchanged.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	var (
		sqliteErr *sqlite.Error
		myErr     *mysqldriver.MySQLError
		pgErr     *pgconn.PgError
	)
	switch {
	case errors.As(err, &sqliteErr):
		switch sqliteErr.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			return errors.Join(ErrDuplicate, err)
		case sqliteConstraintForeignKey:
			return errors.Join(ErrForeignKey, err)
		}
	case errors.As(err, &myErr):
		switch myErr.Number {
		case 1062: // ER_DUP_ENTRY
			return errors.Join(ErrDuplicate, err)
		case 1451, 1452: // ER_ROW_IS_REFERENCED_2, ER_NO_REFERENCED_ROW_2
			return errors.Join(ErrForeignKey, err)
		}
	case errors.As(err, &pgErr):
		switch pgErr.Code {
		case "23505": // unique_violation
			return errors.Join(ErrDuplicate, err)
		case "23503": // foreign_key_violation
			return errors.Join(ErrForeignKey, err)
		}
	}
	return err
}
