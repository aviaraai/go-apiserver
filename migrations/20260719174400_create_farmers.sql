-- +goose Up
CREATE TABLE farmers (
    id            BIGSERIAL PRIMARY KEY,
    public_id     TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    farmer_type   TEXT NOT NULL CHECK (farmer_type IN ('individual', 'small', 'enterprise', 'goshala')),
    relation      TEXT NOT NULL CHECK (relation IN ('son', 'wife')),
    relation_name TEXT NOT NULL,
    phone_number  TEXT NOT NULL UNIQUE CHECK (phone_number ~ '^[0-9]{10}$'),
    state         TEXT NOT NULL,
    district      TEXT NOT NULL,
    mandal        TEXT NOT NULL,
    village       TEXT NOT NULL,
    photo_key     TEXT NOT NULL,
    latitude      DOUBLE PRECISION NOT NULL,
    longitude     DOUBLE PRECISION NOT NULL,
    created_by    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    TEXT NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_farmers_created_by ON farmers(created_by);

-- +goose Down
DROP TABLE farmers;
