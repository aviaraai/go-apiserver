package qr

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrAnimalNotFound      = errors.New("no animal found for godhaar id")
	ErrQRAlreadyExists     = errors.New("qr code already exists for this animal")
	ErrCreateNoRowReturned = errors.New("insert did not return a row")
	ErrQRNotFound          = errors.New("qr code not found for this animal")
)

func translatePGError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505":
			return ErrQRAlreadyExists
		}
	}
	return fmt.Errorf("db error: %w", err)
}
