-- +goose Up
CREATE TABLE animals (
    id                 BIGSERIAL PRIMARY KEY,
    godhaar_id         TEXT NOT NULL UNIQUE,
    farmer_id          BIGINT REFERENCES farmers(id) ON DELETE RESTRICT,
    animal_type        TEXT NOT NULL CHECK (animal_type IN ('Cow', 'Buffalo')),
    gender             TEXT NOT NULL CHECK (gender IN ('Male', 'Female')),
    breed              TEXT NOT NULL,
    age                INT NOT NULL,
    cost               DECIMAL(10, 2),
    insurance_premium  DECIMAL(10, 2),
    state              TEXT NOT NULL,
    district           TEXT NOT NULL,
    mandal             TEXT NOT NULL,
    village            TEXT NOT NULL,
    body_color         TEXT NOT NULL CHECK (body_color IN ('BLACK', 'WHITE', 'BROWN', 'GREY', 'SPOTTED', 'UNKNOWN')),
    muzzle_color       TEXT NOT NULL CHECK (muzzle_color IN ('BLACK', 'PINK', 'MIXED', 'UNKNOWN')),
    health_remarks     TEXT,
    latitude           DOUBLE PRECISION NOT NULL,
    longitude          DOUBLE PRECISION NOT NULL,
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         TEXT NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_animals_created_by ON animals(created_by);
CREATE INDEX idx_animals_farmer_id ON animals(farmer_id);

-- +goose Down
DROP TABLE animals;
