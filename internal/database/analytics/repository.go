package analytics

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Analytics(ctx context.Context) (*Analytics, error) {
	const query = `
		SELECT
			f.total_farmers,
			a.total_animals,
			a.assigned_male,
			a.assigned_female,
			a.unassigned_male,
			a.unassigned_female
		FROM
		(
		    SELECT COUNT(*) AS total_farmers
		    FROM farmers
		) AS f
		CROSS JOIN
		(
		    SELECT
		        COUNT(*) AS total_animals,
		        COUNT(*) FILTER (WHERE farmer_id IS NOT NULL AND gender = 'Male') AS assigned_male,
		        COUNT(*) FILTER (WHERE farmer_id IS NOT NULL AND gender = 'Female') AS assigned_female,
		        COUNT(*) FILTER (WHERE farmer_id IS NULL AND gender = 'Male') AS unassigned_male,
		        COUNT(*) FILTER (WHERE farmer_id IS NULL AND gender = 'Female') AS unassigned_female
		    FROM animals
		) AS a;
	`

	var analytics Analytics
	if err := r.db.GetContext(ctx, &analytics, query); err != nil {
		return nil, fmt.Errorf("analytics: %w", err)
	}
	return &analytics, nil
}

func (r *Repository) UserAnalytics(ctx context.Context, userID string) (*Analytics, error) {
	const query = `
		SELECT
			f.total_farmers,
			a.total_animals,
			a.assigned_male,
			a.assigned_female,
			a.unassigned_male,
			a.unassigned_female
		FROM
		(
		    SELECT COUNT(*) AS total_farmers
		    FROM farmers WHERE created_by = $1
		) AS f
		CROSS JOIN
		(
		    SELECT
		        COUNT(*) AS total_animals,
		        COUNT(*) FILTER (WHERE farmer_id IS NOT NULL AND gender = 'Male') AS assigned_male,
		        COUNT(*) FILTER (WHERE farmer_id IS NOT NULL AND gender = 'Female') AS assigned_female,
		        COUNT(*) FILTER (WHERE farmer_id IS NULL AND gender = 'Male') AS unassigned_male,
		        COUNT(*) FILTER (WHERE farmer_id IS NULL AND gender = 'Female') AS unassigned_female
		    FROM animals WHERE created_by = $1
		) AS a;
	`

	var analytics Analytics
	if err := r.db.GetContext(ctx, &analytics, query, userID); err != nil {
		return nil, fmt.Errorf("user analytics: %w", err)
	}
	return &analytics, nil
}
