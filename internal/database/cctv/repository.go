package cctv

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

// ListGoshalas returns the sites an admin can run an analysis against.
func (r *Repository) ListGoshalas(ctx context.Context) ([]Goshala, error) {
	const query = `
		SELECT id, public_id, name, state, district, mandal, village,
		       photo_key, latitude, longitude
		FROM farmers
		WHERE farmer_type = 'goshala'
		ORDER BY name ASC, id ASC;`

	var rows []Goshala
	if err := r.db.SelectContext(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("list goshalas: %w", err)
	}
	return rows, nil
}

// GoshalaByPublicID resolves the id an analysis request hangs off.
//
// The farmer_type filter is part of the lookup rather than a check afterwards,
// so a request naming a farmer who is not a goshala is rejected by the same
// path as one naming nobody at all.
func (r *Repository) GoshalaByPublicID(ctx context.Context, publicID string) (*Goshala, error) {
	const query = `
		SELECT id, public_id, name, state, district, mandal, village,
		       photo_key, latitude, longitude
		FROM farmers
		WHERE public_id = $1 AND farmer_type = 'goshala';`

	var g Goshala
	if err := r.db.GetContext(ctx, &g, query, publicID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGoshalaNotFound
		}
		return nil, fmt.Errorf("get goshala %s: %w", publicID, err)
	}
	return &g, nil
}

// OpenRequest records that an admin asked for an analysis, before any work
// starts. Returns the new row's id, which every later update keys off.
func (r *Repository) OpenRequest(ctx context.Context, p CreateRequest) (int64, error) {
	const query = `
		INSERT INTO cctv_video_analytics (farmer_id, status, requested_by, requested_by_email)
		VALUES (:farmer_id, 'running', :requested_by, :requested_by_email)
		RETURNING id;`

	rows, err := r.db.NamedQueryContext(ctx, query, p)
	if err != nil {
		return 0, fmt.Errorf("open cctv request: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("open cctv request: %w", err)
		}
		return 0, fmt.Errorf("open cctv request: no id returned")
	}
	var id int64
	if err := rows.Scan(&id); err != nil {
		return 0, fmt.Errorf("scan cctv request id: %w", err)
	}
	return id, nil
}

func (r *Repository) CompleteRequest(ctx context.Context, p CompleteRequest) error {
	const query = `
		UPDATE cctv_video_analytics
		SET status = 'succeeded',
		    inference_job_id = :inference_job_id,
		    source_video_key = :source_video_key,
		    annotated_video_key = :annotated_video_key,
		    total_animals = :total_animals,
		    total_clear_animals = :total_clear_animals,
		    detail = :detail::jsonb,
		    completed_at = now()
		WHERE id = :id AND status = 'running';`

	return r.exec(ctx, query, p, "complete cctv request")
}

func (r *Repository) FailRequest(ctx context.Context, p FailRequest) error {
	const query = `
		UPDATE cctv_video_analytics
		SET status = 'failed',
		    inference_job_id = :inference_job_id,
		    source_video_key = :source_video_key,
		    error_code = :error_code,
		    detail = :detail::jsonb,
		    completed_at = now()
		WHERE id = :id AND status = 'running';`

	return r.exec(ctx, query, p, "fail cctv request")
}

// exec runs a named update and insists it touched a row. The status = 'running'
// guard on both callers means a miss is a real anomaly — a request closed twice,
// or closed by something else — not a routine no-op worth swallowing.
func (r *Repository) exec(ctx context.Context, query string, arg any, what string) error {
	res, err := r.db.NamedExecContext(ctx, query, arg)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if affected == 0 {
		return ErrRequestNotFound
	}
	return nil
}

// ListRequests returns the analysis history, newest first, optionally for one
// goshala. The goshala's details come from the join so a renamed site reads
// correctly across its whole history.
func (r *Repository) ListRequests(ctx context.Context, goshalaPublicID *string) ([]RequestRow, error) {
	const query = `
		SELECT c.id, c.status, c.inference_job_id,
		       c.source_video_key, c.annotated_video_key,
		       c.total_animals, c.total_clear_animals,
		       c.error_code, c.detail,
		       c.requested_by, c.requested_by_email,
		       c.requested_at, c.completed_at,
		       c.farmer_id,
		       f.public_id AS goshala_public_id,
		       f.name      AS goshala_name,
		       f.village   AS goshala_village,
		       f.mandal    AS goshala_mandal,
		       f.district  AS goshala_district,
		       f.state     AS goshala_state
		FROM cctv_video_analytics c
		JOIN farmers f ON f.id = c.farmer_id
		WHERE ($1::text IS NULL OR f.public_id = $1)
		ORDER BY c.requested_at DESC, c.id DESC;`

	var rows []RequestRow
	if err := r.db.SelectContext(ctx, &rows, query, goshalaPublicID); err != nil {
		return nil, fmt.Errorf("list cctv requests: %w", err)
	}
	return rows, nil
}
