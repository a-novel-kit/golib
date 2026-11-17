package postgres

import (
	"errors"

	"github.com/uptrace/bun/driver/pgdriver"
)

// SQLState returns the SQLSTATE code and the constraint name of the PostgreSQL error err wraps, such
// as "23505" and "users_email_key" for a write that broke a unique index. Both are empty when err
// wraps no PostgreSQL error, and the constraint is empty for an error that names none.
func SQLState(err error) (string, string) {
	serverErr, ok := errors.AsType[pgdriver.Error](err)
	if !ok {
		return "", ""
	}

	return serverErr.Field('C'), serverErr.Field('n')
}
