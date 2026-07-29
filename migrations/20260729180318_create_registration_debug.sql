-- +goose Up
CREATE TABLE animal_registration_debug (
    id BIGSERIAL PRIMARY KEY,
    image_folder TEXT UNIQUE NOT NULL,
    inference_info TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE animal_registration_debug;
