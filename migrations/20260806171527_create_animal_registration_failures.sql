-- +goose Up
CREATE TABLE animal_registration_failures (
    id                  BIGSERIAL PRIMARY KEY,
    error_code          TEXT NOT NULL,
    image_keys          JSONB NOT NULL DEFAULT '[]',
    app_version         TEXT,
    os_version          TEXT,
    device_model        TEXT,
    device_manufacturer TEXT,
    detail              JSONB NOT NULL DEFAULT '{}',
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_arf_error_created ON animal_registration_failures (error_code, created_at DESC);
CREATE INDEX idx_arf_created_at_id ON animal_registration_failures (created_at DESC, id DESC);
CREATE INDEX idx_arf_created_by_created_at ON animal_registration_failures (created_by, created_at DESC);

-- +goose Down
DROP TABLE animal_registration_failures;
