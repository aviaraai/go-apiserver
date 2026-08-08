-- +goose Up
ALTER TABLE animals ADD COLUMN tag_id TEXT UNIQUE;

-- +goose Down
ALTER TABLE animals DROP COLUMN tag_id;
