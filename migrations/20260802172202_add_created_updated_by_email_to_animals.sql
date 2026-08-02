-- +goose Up
ALTER TABLE animals ADD COLUMN created_by_email TEXT;
ALTER TABLE animals ADD COLUMN updated_by_email TEXT;

-- +goose Down
ALTER TABLE animals DROP COLUMN created_by_email;
ALTER TABLE animals DROP COLUMN updated_by_email;
