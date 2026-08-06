-- +goose Up
ALTER TABLE farmers ALTER COLUMN relation DROP NOT NULL;
ALTER TABLE farmers ALTER COLUMN relation_name DROP NOT NULL;

-- +goose Down
ALTER TABLE farmers ALTER COLUMN relation SET NOT NULL;
ALTER TABLE farmers ALTER COLUMN relation_name SET NOT NULL;
