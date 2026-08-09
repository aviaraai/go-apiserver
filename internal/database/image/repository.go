package image

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

func (r *Repository) ImagesByAnimalID(ctx context.Context, animalID int64) ([]ImageRow, error) {
	const query = `SELECT image_type, image_key FROM images WHERE animal_id=$1 AND image_type IN ('muzzle', 'front') AND sequence IN (1, 2, 3);`
	var images []ImageRow
	if err := r.db.SelectContext(ctx, &images, query, animalID); err != nil {
		return nil, fmt.Errorf("get images for godhaar id %d: %w", animalID, err)
	}
	return images, nil
}
