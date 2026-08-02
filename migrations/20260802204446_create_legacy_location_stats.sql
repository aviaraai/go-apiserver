-- +goose Up
CREATE TABLE legacy_location_stats (
    id BIGSERIAL PRIMARY KEY,
    state TEXT NOT NULL,
    district TEXT NOT NULL,
    mandal TEXT NOT NULL,
    farmer_count INTEGER NOT NULL DEFAULT 0,
    animal_count INTEGER NOT NULL DEFAULT 0,

    CONSTRAINT legacy_location_stats_location_key
        UNIQUE (state, district, mandal),

    CONSTRAINT legacy_location_stats_counts_non_neg
        CHECK (farmer_count >= 0 AND animal_count >= 0)
);

-- +goose Down
DROP TABLE legacy_location_stats;
