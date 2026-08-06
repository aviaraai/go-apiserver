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
			detail, created_by
		) VALUES (
			:error_code, :image_keys::jsonb,
			:app_version, :os_version, :device_model, :device_manufacturer,
			:detail::jsonb, :created_by
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
			detail, created_by
		) VALUES (
			:decision, :godhaar_id, :score, :error_code, :image_keys::jsonb,
			:app_version, :os_version, :device_model, :device_manufacturer,
			:detail::jsonb, :created_by
		)`

	if _, err := r.db.NamedExecContext(ctx, query, p); err != nil {
		return fmt.Errorf("record search: %w", err)
	}
	return nil
}

func (r *Repository) ListRegistrationFailures(ctx context.Context) ([]RegistrationFailureRow, error) {
	const query = `
		SELECT id, error_code, image_keys,
		       app_version, os_version, device_model, device_manufacturer,
		       detail, created_by, created_at
		FROM animal_registration_failures
		ORDER BY created_at DESC, id DESC;`

	var rows []RegistrationFailureRow
	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("list registration failures: %w", err)
	}
	return rows, nil
}

// ListSearches returns every search attempt, newest first, with the matched
// animal's current details pulled in by join rather than copied into the record
// at write time. The front image is picked out the same way the mobile animal
// listing does it.
func (r *Repository) ListSearches(ctx context.Context) ([]SearchRecordRow, error) {
	const query = `
		SELECT s.id, s.decision, s.godhaar_id, s.score, s.error_code, s.verified,
		       s.image_keys,
		       s.app_version, s.os_version, s.device_model, s.device_manufacturer,
		       s.detail, s.created_by, s.created_at,
		       a.animal_type  AS matched_type,
		       a.breed        AS matched_breed,
		       a.gender       AS matched_gender,
		       a.age          AS matched_age,
		       a.body_color   AS matched_body_color,
		       a.muzzle_color AS matched_muzzle_color,
		       a.horn_shape   AS matched_horn_shape,
		       a.village      AS matched_village,
		       a.mandal       AS matched_mandal,
		       a.district     AS matched_district,
		       a.state        AS matched_state,
		       i.image_key    AS matched_image_key
		FROM animal_search_records s
		LEFT JOIN animals a ON a.godhaar_id = s.godhaar_id
		LEFT JOIN images  i ON i.animal_id = a.id AND i.image_type = 'front' AND i.sequence = 1
		ORDER BY s.created_at DESC, s.id DESC;`

	var rows []SearchRecordRow
	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("list searches: %w", err)
	}
	return rows, nil
}

// UpdateSearchVerification sets the human verdict on a matched search. It is
// freely reversible — yes can become no and back — because a dashboard check is
// exactly the kind of judgement that gets revised.
//
// The decision = 'MATCH' guard in the WHERE clause is what makes the two
// outcomes distinguishable: a row that exists but is not a match reports
// ErrNotVerifiable rather than a bare not-found.
func (r *Repository) UpdateSearchVerification(ctx context.Context, id int64, verified string) (*SearchRecordRow, error) {
	const query = `
		UPDATE animal_search_records
		SET verified = $2
		WHERE id = $1 AND decision = 'MATCH'
		RETURNING id, decision, godhaar_id, score, error_code, verified,
		          image_keys,
		          app_version, os_version, device_model, device_manufacturer,
		          detail, created_by, created_at;`

	var row SearchRecordRow
	err := r.db.GetContext(ctx, &row, query, id, verified)
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
		`SELECT decision FROM animal_search_records WHERE id = $1;`, id); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrRecordNotFound
	case err != nil:
		return nil, fmt.Errorf("look up search record: %w", err)
	default:
		return nil, ErrNotVerifiable
	}
}
