package postgres

import (
	"strings"
)

// isDuplicateKey checks if a PostgreSQL error is a unique constraint violation.
func isDuplicateKey(err error) bool {
	// pgx wraps errors; check for the SQLSTATE 23505 (unique_violation).
	return err != nil && strings.Contains(err.Error(), "23505")
}
