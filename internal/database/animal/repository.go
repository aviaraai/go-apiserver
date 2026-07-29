package animal

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

func (r *Repository) AnimalsByFarmerID(ctx context.Context, farmerID string) ([]Animal, error) {
	const query = `SELECT a.id, a.godhaar_id, a.animal_type, a.gender, a.breed, a.age, a.cost, a.insurance_premium, a.state, a.district, a.mandal, a.village, i.image_key FROM farmers AS f join animals AS a ON f.id = a.farmer_id JOIN images AS i ON a.id=i.animal_id WHERE f.public_id=$1 AND i.image_type='front' AND i.sequence=1;`

	var animals []Animal
	if err := r.db.SelectContext(ctx, &animals, query, farmerID); err != nil {
		return nil, fmt.Errorf("list animals: %w", err)
	}
	return animals, nil
}

func (r *Repository) UnassignedAnimalsByUser(ctx context.Context, userID string) ([]Animal, error) {
	const query = `SELECT a.id, a.godhaar_id, a.animal_type, a.gender, a.breed, a.age, a.cost, a.insurance_premium, a.state, a.district, a.mandal, a.village, i.image_key FROM animals AS a JOIN images AS i ON a.id=i.animal_id WHERE a.created_by=$1 AND a.farmer_id IS NULL AND i.image_type='front' AND i.sequence=1;`

	var animals []Animal
	if err := r.db.SelectContext(ctx, &animals, query, userID); err != nil {
		return nil, fmt.Errorf("list animals: %w", err)
	}
	return animals, nil
}

func (r *Repository) GetAnimal(ctx context.Context, godhaarID string) (*Animal, error) {
	const query = `SELECT f.public_id, a.godhaar_id, a.animal_type, a.gender, a.breed, a.age, a.cost, a.insurance_premium, a.state, a.district, a.mandal, a.village FROM animals AS a LEFT JOIN farmers AS f ON f.id=a.farmer_id WHERE godhaar_id=$1;`

	var animal Animal
	if err := r.db.GetContext(ctx, &animal, query, godhaarID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRowNotFound
		}
		return nil, fmt.Errorf("get farmer: %w", err)
	}
	return &animal, nil
}

func (r *Repository) FarmerIDByPublicID(ctx context.Context, publicID string) (*int64, error) {
	const query = `SELECT id FROM farmers WHERE public_id=$1;`
	var farmerID *int64
	if err := r.db.GetContext(ctx, &farmerID, query, publicID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFarmerNotFound
		}
		return nil, fmt.Errorf("get farmer id for public id %s: %w", publicID, err)
	}
	return farmerID, nil
}

func (r *Repository) FindFAISSCandidates(ctx context.Context, lat, lng, bbox float64) ([]CandidateRow, error) {
	const query = `
	SELECT
    e.faiss_id,
    a.godhaar_id,
    a.latitude,
    a.longitude,
    a.body_color,
    a.muzzle_color
	FROM embeddings e
	JOIN animals a ON e.animal_id = a.id
	WHERE a.latitude IS NOT NULL
  AND a.longitude IS NOT NULL
  AND a.latitude BETWEEN $1 AND $2
  AND a.longitude BETWEEN $3 AND $4;
	`

	var rows []CandidateRow
	err := r.db.SelectContext(ctx, &rows, query,
		lat-bbox, lat+bbox, lng-bbox, lng+bbox)
	if err != nil {
		return nil, fmt.Errorf("find faiss candidates: %w", err)
	}
	return rows, nil
}

func (r *Repository) CreateAnimalWithEmbeddingsAndImages(ctx context.Context, params CreateAnimalTx) (*Animal, error) {
	const insertAnimalReturning = `
	INSERT INTO animals (
    godhaar_id, farmer_id, animal_type, gender, breed, age, cost, insurance_premium,
    state, district, mandal, village, latitude, longitude, health_remarks,
    body_color, muzzle_color, created_by, updated_by
	) VALUES (
    :godhaar_id, :farmer_id, :animal_type, :gender, :breed, :age, :cost, :insurance_premium,
    :state, :district, :mandal, :village, :latitude, :longitude, :health_remarks,
    :body_color, :muzzle_color, :created_by, :updated_by
	) RETURNING id, godhaar_id, animal_type, gender, breed, age, cost, insurance_premium, state, district, mandal, village;
	`

	const insertEmbedding = `INSERT INTO embeddings (animal_id, embedding_type, sequence, faiss_id) VALUES (:animal_id, :embedding_type, :sequence, :faiss_id);`
	const insertImage = `INSERT INTO images (animal_id, image_type, sequence, image_key) VALUES (:animal_id, :image_type, :sequence, :image_key);`

	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := sqlx.NamedQueryContext(ctx, tx, insertAnimalReturning, params.Animal)
	if err != nil {
		return nil, translatePGError(err)
	}

	var animal Animal
	if !rows.Next() {
		defer rows.Close()
		if err := rows.Err(); err != nil {
			return nil, translatePGError(err)
		}
		return nil, ErrCreateNoRowReturned
	}
	if err := rows.StructScan(&animal); err != nil {
		rows.Close()
		return nil, fmt.Errorf("scan animal: %w", err)
	}
	rows.Close()

	for _, e := range params.Embeddings {
		row := struct {
			AnimalID      int64  `db:"animal_id"`
			EmbeddingType string `db:"embedding_type"`
			Sequence      int    `db:"sequence"`
			FaissID       int64  `db:"faiss_id"`
		}{animal.AnimalID, e.EmbeddingType, e.Sequence, e.FaissID}
		if _, err := tx.NamedExecContext(ctx, insertEmbedding, row); err != nil {
			return nil, fmt.Errorf("insert embedding %s: %w", e.EmbeddingType, err)
		}
	}

	for _, img := range params.Images {
		row := struct {
			AnimalID  int64  `db:"animal_id"`
			ImageType string `db:"image_type"`
			Sequence  int    `db:"sequence"`
			ImageKey  string `db:"image_key"`
		}{animal.AnimalID, img.ImageType, img.Sequence, img.ImageKey}
		if _, err := tx.NamedExecContext(ctx, insertImage, row); err != nil {
			return nil, fmt.Errorf("insert image %s: %w", img.ImageType, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &animal, nil
}

func (r *Repository) AddDebugAnimal(ctx context.Context, p DebugCreateParams) error {
	const query = `
		INSERT INTO animal_registration_debug (
			image_folder,
			inference_info,
			created_by
		)
		VALUES (
			:image_folder,
			:inference_info,
			:created_by
		)`

	_, err := r.db.NamedExecContext(ctx, query, p)
	if err != nil {
		return fmt.Errorf("create registration debug: %w", err)
	}

	return nil
}
