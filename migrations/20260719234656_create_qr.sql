-- +goose Up
CREATE TABLE qr (
    id                 BIGSERIAL PRIMARY KEY,
    animal_id          BIGINT NOT NULL UNIQUE REFERENCES animals(id) ON DELETE RESTRICT,
    qr_image_key TEXT NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE qr;
