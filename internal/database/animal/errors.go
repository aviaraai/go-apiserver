package animal

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrInvalidAnimalData   = errors.New("invalid animal details")
	ErrCreateNoRowReturned = errors.New("insert did not return a row")
	ErrRowNotFound         = errors.New("no animal details found")
	ErrFarmerNotFound      = errors.New("farmer not found")
)

func translatePGError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23502", "23514":
			return fmt.Errorf("%w: %w", ErrInvalidAnimalData, err)
		}
	}
	return fmt.Errorf("db error: %w", err)
}
