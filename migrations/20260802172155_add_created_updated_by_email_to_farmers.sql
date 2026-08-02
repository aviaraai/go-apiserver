-- +goose Up
ALTER TABLE farmers ADD COLUMN created_by_email TEXT;
ALTER TABLE farmers ADD COLUMN updated_by_email TEXT;

-- +goose Down
ALTER TABLE farmers DROP COLUMN created_by_email;
ALTER TABLE farmers DROP COLUMN updated_by_email;
