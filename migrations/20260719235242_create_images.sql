-- +goose Up
CREATE TABLE images (
    id                 BIGSERIAL PRIMARY KEY,
    animal_id          BIGINT NOT NULL REFERENCES animals(id) ON DELETE RESTRICT,
    image_type TEXT NOT NULL CHECK (image_type IN ('front', 'muzzle', 'left', 'right', 'insurance', 'medical_cert', 'prescription', 'valuation_cert')),
    sequence INT NOT NULL DEFAULT 1,
    image_key TEXT NOT NULL,
    uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(animal_id, image_type, sequence)
);
CREATE INDEX idx_images_animal_id ON images(animal_id);

-- +goose Down
DROP TABLE images;
