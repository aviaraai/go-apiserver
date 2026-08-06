-- +goose Up
ALTER TABLE animals ADD COLUMN horn_shape TEXT CHECK (horn_shape IN ('STRAIGHT', 'CURVED', 'UNKNOWN'));

-- +goose Down
ALTER TABLE animals DROP COLUMN horn_shape;
