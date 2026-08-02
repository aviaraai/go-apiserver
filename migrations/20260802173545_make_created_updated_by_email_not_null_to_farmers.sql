-- +goose Up
ALTER TABLE farmers ALTER COLUMN created_by_email SET NOT NULL;
ALTER TABLE farmers ALTER COLUMN updated_by_email SET NOT NULL;

-- +goose Down
ALTER TABLE farmers ALTER COLUMN created_by_email DROP NOT NULL;
ALTER TABLE farmers ALTER COLUMN updated_by_email DROP NOT NULL;
