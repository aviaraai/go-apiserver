package farmer

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

func (r *Repository) Create(ctx context.Context, q *CreateFarmer) (*Farmer, error) {
	const query = `INSERT INTO farmers (public_id, name, farmer_type, relation, relation_name, phone_number, state, district, mandal, village, photo_key, latitude, longitude, created_by, updated_by) VALUES (:public_id, :name, :farmer_type, :relation, :relation_name, :phone_number, :state, :district, :mandal, :village, :photo_key, :latitude, :longitude, :created_by, :updated_by) RETURNING public_id, name, farmer_type, relation, relation_name, phone_number, state, district, mandal, village;`
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

	var farmer Farmer
	if err := rows.StructScan(&farmer); err != nil {
		return nil, fmt.Errorf("scan farmer: %w", err)
	}
	return &farmer, nil
}

func (r *Repository) ListAll(ctx context.Context) ([]Farmer, error) {
	const query = `SELECT public_id, name, farmer_type, relation, relation_name, phone_number, state, district, mandal, village FROM farmers;`
	var farmers []Farmer
	if err := r.db.SelectContext(ctx, &farmers, query); err != nil {
		return nil, fmt.Errorf("list farmers: %w", err)
	}
	return farmers, nil
}

func (r *Repository) FarmersByPhoneNumber(ctx context.Context, phoneNumber string) ([]Farmer, error) {
	const query = `SELECT public_id, name, farmer_type, relation, relation_name, phone_number, state, district, mandal, village FROM farmers WHERE phone_number=$1;`
	var farmers []Farmer
	if err := r.db.SelectContext(ctx, &farmers, query, phoneNumber); err != nil {
		return nil, fmt.Errorf("list farmers: %w", err)
	}
	return farmers, nil
}

func (r *Repository) FarmersByUser(ctx context.Context, userID string) ([]Farmer, error) {
	const query = `SELECT public_id, name, farmer_type, relation, relation_name, state, district, mandal, village, phone_number FROM farmers WHERE created_by=$1;`
	var farmers []Farmer
	if err := r.db.SelectContext(ctx, &farmers, query, userID); err != nil {
		return nil, fmt.Errorf("list farmers: %w", err)
	}
	return farmers, nil
}

func (r *Repository) Get(ctx context.Context, farmer_id string) (*Farmer, error) {
	const query = `SELECT public_id, name, farmer_type, relation, relation_name, phone_number, state, district, mandal, village, photo_key FROM farmers WHERE public_id=$1;`
	var farmer Farmer
	if err := r.db.GetContext(ctx, &farmer, query, farmer_id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRowNotFound
		}
		return nil, fmt.Errorf("get farmer: %w", err)
	}
	return &farmer, nil
}
