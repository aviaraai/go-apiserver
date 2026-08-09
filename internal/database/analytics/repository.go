package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type Repository struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UserAnalytics(ctx context.Context, userID string) (*UserAnalytics, error) {
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

	var analytics UserAnalytics
	if err := r.db.GetContext(ctx, &analytics, query, userID); err != nil {
		return nil, fmt.Errorf("user analytics: %w", err)
	}
	return &analytics, nil
}

func (r *Repository) AdminAnalytics(ctx context.Context, state, district, mandal, breed *string, fromDate, toDate *time.Time) ([]AdminAnalytics, error) {
	const query = `
		SELECT
	    COALESCE(f.created_by_email, a.created_by_email) AS user_email,
	    COALESCE(f.total_farmers, 0)         AS total_farmers,
	    COALESCE(a.total_animals, 0)         AS total_animals,
	    COALESCE(a.total_assigned, 0)        AS total_assigned,
	    COALESCE(a.total_unassigned, 0)      AS total_unassigned
		FROM
	    (
	        SELECT
	            created_by_email,
	            COUNT(*) AS total_farmers
	        FROM farmers
	        WHERE
	            ($1::text IS NULL OR state = $1)
	            AND ($2::text IS NULL OR district = $2)
	            AND ($3::text IS NULL OR mandal = $3)
	            AND ($5::timestamptz IS NULL OR created_at >= $5)
	            AND ($6::timestamptz IS NULL OR created_at <  $6)
	        GROUP BY created_by_email
	    ) AS f
		FULL OUTER JOIN
	    (
	        SELECT
	            created_by_email,
	            COUNT(*) AS total_animals,
	            COUNT(*) FILTER (WHERE farmer_id IS NOT NULL) AS total_assigned,
	            COUNT(*) FILTER (WHERE farmer_id IS NULL)     AS total_unassigned
	        FROM animals
	        WHERE
	            ($1::text IS NULL OR state = $1)
	            AND ($2::text IS NULL OR district = $2)
	            AND ($3::text IS NULL OR mandal = $3)
				AND ($4::text IS NULL OR breed = $4)
	            AND ($5::timestamptz IS NULL OR created_at >= $5)
	            AND ($6::timestamptz IS NULL OR created_at <  $6)
	        GROUP BY created_by_email
	    ) AS a
	    ON f.created_by_email = a.created_by_email
		ORDER BY total_animals DESC, user_email ASC;
	`

	var analytics []AdminAnalytics
	if err := r.db.SelectContext(ctx, &analytics, query, state, district, mandal, breed, fromDate, toDate); err != nil {
		return nil, fmt.Errorf("admin analytics: %w", err)
	}
	return analytics, nil
}

func (r *Repository) AdminTotalAnalytics(ctx context.Context) (*AdminTotalAnalytics, error) {
	const query = `
		SELECT
			(SELECT COUNT(*) FROM farmers) AS total_farmers,
			(SELECT COUNT(*) FROM animals) AS total_animals;
	`

	var analytics AdminTotalAnalytics
	if err := r.db.GetContext(ctx, &analytics, query); err != nil {
		return nil, fmt.Errorf("total admin analytics: %w", err)
	}
	return &analytics, nil
}

func (r *Repository) LegacyAnalytics(ctx context.Context, state, district, mandal *string) ([]LegacyAnalytics, error) {
	const query = `
		SELECT state, district, mandal, farmer_count, animal_count
		FROM legacy_location_stats
		WHERE ($1::text IS NULL OR state = $1)
		  AND ($2::text IS NULL OR district = $2)
		  AND ($3::text IS NULL OR mandal = $3)
		ORDER BY animal_count DESC, farmer_count DESC, mandal ASC;
	`

	var legacyAnalytics []LegacyAnalytics
	if err := r.db.SelectContext(ctx, &legacyAnalytics, query, state, district, mandal); err != nil {
		return nil, fmt.Errorf("legacy analytics: %w", err)
	}
	return legacyAnalytics, nil
}
