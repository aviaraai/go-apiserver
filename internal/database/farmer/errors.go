package farmer

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrDuplicatePhone      = errors.New("farmer with this phone number already exists")
	ErrInvalidFarmerData   = errors.New("invalid farmer details")
	ErrCreateNoRowReturned = errors.New("insert did not return a row")
	ErrRowNotFound         = errors.New("no farmer details found")
)

func translatePGError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505":
			return ErrDuplicatePhone
		case "23502", "23514":
			return ErrInvalidFarmerData
		}
	}
	return fmt.Errorf("db error: %w", err)
}
