-- +goose Up
ALTER TABLE animals ALTER COLUMN created_by_email SET NOT NULL;
ALTER TABLE animals ALTER COLUMN updated_by_email SET NOT NULL;

-- +goose Down
ALTER TABLE animals ALTER COLUMN created_by_email DROP NOT NULL;
ALTER TABLE animals ALTER COLUMN updated_by_email DROP NOT NULL;
