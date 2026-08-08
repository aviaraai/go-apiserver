package debug

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// ErrRecordNotFound is returned when an update targets a row that does not
// exist, or one that is not eligible for it.
var ErrRecordNotFound = errors.New("debug record not found")

// ErrNotVerifiable is returned when a verification is attempted on a search
// that never produced a match, so there is nothing to confirm or refute.
var ErrNotVerifiable = errors.New("search record has no match to verify")

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) RecordRegistrationFailure(ctx context.Context, p CreateRegistrationFailure) error {
	const query = `
		INSERT INTO animal_registration_failures (
			error_code, image_keys,
			app_version, os_version, device_model, device_manufacturer,
			detail, created_by, created_by_email
		) VALUES (
			:error_code, :image_keys,
			:app_version, :os_version, :device_model, :device_manufacturer,
			:detail, :created_by, :created_by_email
		)`

	if _, err := r.db.NamedExecContext(ctx, query, p); err != nil {
		return fmt.Errorf("record registration failure: %w", err)
	}
	return nil
}

func (r *Repository) RecordSearch(ctx context.Context, p CreateSearchRecord) error {
	const query = `
		INSERT INTO animal_search_records (
			decision, godhaar_id, score, error_code, image_keys,
			app_version, os_version, device_model, device_manufacturer,
			detail, created_by, created_by_email
		) VALUES (
			:decision, :godhaar_id, :score, :error_code, :image_keys,
			:app_version, :os_version, :device_model, :device_manufacturer,
			:detail, :created_by, :created_by_email
		)`

	if _, err := r.db.NamedExecContext(ctx, query, p); err != nil {
		return fmt.Errorf("record search: %w", err)
	}
	return nil
}

// ListRegistrationFailures returns every failure, newest first, without the
// detail payload. Read RegistrationFailureByID for that.
func (r *Repository) ListRegistrationFailures(ctx context.Context) ([]RegistrationFailureListRow, error) {
	const query = `
		SELECT registration_id::text AS registration_id, error_code, image_keys,
		       app_version, os_version, device_model, device_manufacturer,
		       created_by, created_by_email, created_at
		FROM animal_registration_failures
		ORDER BY created_at DESC, id DESC;`

	var rows []RegistrationFailureListRow
	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("list registration failures: %w", err)
	}
	return rows, nil
}

func (r *Repository) RegistrationFailureByID(ctx context.Context, registrationID string) (*RegistrationFailureRow, error) {
	const query = `
		SELECT registration_id::text AS registration_id, error_code, image_keys,
		       app_version, os_version, device_model, device_manufacturer,
		       detail, created_by, created_by_email, created_at
		FROM animal_registration_failures
		WHERE registration_id = $1::uuid;`

	var row RegistrationFailureRow
	if err := r.db.GetContext(ctx, &row, query, registrationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("get registration failure %s: %w", registrationID, err)
	}
	return &row, nil
}

// ListSearches returns every search attempt, newest first. No join: decision,
// verified and godhaar_id are all on the record, which is everything the
// dashboard filters on.
func (r *Repository) ListSearches(ctx context.Context) ([]SearchRecordListRow, error) {
	const query = `
		SELECT search_id::text AS search_id, decision, godhaar_id, score, error_code, verified, image_keys,
		       app_version, os_version, device_model, device_manufacturer,
		       created_by, created_by_email, created_at
		FROM animal_search_records
		ORDER BY created_at DESC, id DESC;`

	var rows []SearchRecordListRow
	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("list searches: %w", err)
	}
	return rows, nil
}

func (r *Repository) SearchByID(ctx context.Context, searchID string) (*SearchRecordRow, error) {
	const query = `
		SELECT search_id::text AS search_id, decision, godhaar_id, score, error_code, verified, image_keys,
		       app_version, os_version, device_model, device_manufacturer,
		       detail, created_by, created_by_email, created_at
		FROM animal_search_records
		WHERE search_id = $1::uuid;`

	var row SearchRecordRow
	if err := r.db.GetContext(ctx, &row, query, searchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, fmt.Errorf("get search record %s: %w", searchID, err)
	}
	return &row, nil
}

// MatchedAnimalImages returns the identifying photos of the animal a search
// matched — the four slots the model compares against, never the certificates.
//
// found is false when the godhaar_id no longer resolves. That is a real state
// worth reporting rather than an error: the search record stands as evidence of
// what the model said even after the animal it named has been deleted. An
// animal that exists but has no images returns found with an empty slice, which
// is why existence is checked separately from the image rows.
func (r *Repository) MatchedAnimalImages(ctx context.Context, godhaarID string) ([]AnimalImage, bool, error) {
	var animalID int64
	err := r.db.GetContext(ctx, &animalID,
		`SELECT id FROM animals WHERE godhaar_id = $1;`, godhaarID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("look up matched animal %s: %w", godhaarID, err)
	}

	const query = `
		SELECT image_type, sequence, image_key
		FROM images
		WHERE animal_id = $1 AND image_type IN ('front', 'muzzle', 'left', 'right')
		ORDER BY image_type, sequence;`

	var images []AnimalImage
	if err := r.db.SelectContext(ctx, &images, query, animalID); err != nil {
		return nil, false, fmt.Errorf("list images for matched animal %s: %w", godhaarID, err)
	}
	return images, true, nil
}

// UpdateSearchVerification sets the human verdict on a matched search. It is
// freely reversible — yes can become no and back — because a dashboard check is
// exactly the kind of judgement that gets revised.
//
// The decision = 'MATCH' guard in the WHERE clause is what makes the two
// outcomes distinguishable: a row that exists but is not a match reports
// ErrNotVerifiable rather than a bare not-found.
func (r *Repository) UpdateSearchVerification(ctx context.Context, searchID, verified string) (*SearchRecordRow, error) {
	const query = `
		UPDATE animal_search_records
		SET verified = $2
		WHERE search_id = $1::uuid AND decision = 'MATCH'
		RETURNING search_id::text AS search_id, decision, godhaar_id, score, error_code, verified,
		          image_keys,
		          app_version, os_version, device_model, device_manufacturer,
		          detail, created_by, created_by_email, created_at;`

	var row SearchRecordRow
	err := r.db.GetContext(ctx, &row, query, searchID, verified)
	if err == nil {
		return &row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("update search verification: %w", err)
	}

	// Nothing was updated. Work out which of the two reasons it was, so the
	// caller can answer 404 or 409 rather than guessing.
	var decision string
	switch err := r.db.GetContext(ctx, &decision,
		`SELECT decision FROM animal_search_records WHERE search_id = $1::uuid;`, searchID); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrRecordNotFound
	case err != nil:
		return nil, fmt.Errorf("look up search record: %w", err)
	default:
		return nil, ErrNotVerifiable
	}
}
