package qr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) AnimalIDByGodhaarID(ctx context.Context, godhaarID string) (int64, error) {
	const query = `SELECT id FROM animals WHERE godhaar_id = $1;`
	var animalID int64
	if err := r.db.GetContext(ctx, &animalID, query, godhaarID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrAnimalNotFound
		}
		return 0, fmt.Errorf("get animal id for godhaar id %s: %w", godhaarID, err)
	}
	return animalID, nil
}

func (r *Repository) QRByAnimalID(ctx context.Context, animalID int64) (*QRRow, error) {
	const query = `SELECT animal_id, qr_image_key FROM qr WHERE animal_id = $1;`
	var row QRRow
	if err := r.db.GetContext(ctx, &row, query, animalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQRNotFound
		}
		return nil, fmt.Errorf("get qr for animal id %d: %w", animalID, err)
	}
	return &row, nil
}

func (r *Repository) Create(ctx context.Context, q *CreateQR) (*QRRow, error) {
	const query = `INSERT INTO qr (animal_id, qr_image_key) VALUES (:animal_id, :qr_image_key) RETURNING animal_id, qr_image_key;`

	rows, err := r.db.NamedQueryContext(ctx, query, q)
	if err != nil {
		return nil, translatePGError(err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, translatePGError(err)
		}
		return nil, ErrCreateNoRowReturned
	}

	var qr QRRow
	if err := rows.StructScan(&qr); err != nil {
		return nil, fmt.Errorf("scan qr: %w", err)
	}
	return &qr, nil
}
