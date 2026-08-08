-- +goose Up
ALTER TABLE animal_registration_failures ADD COLUMN created_by_email TEXT;
ALTER TABLE animal_search_records ADD COLUMN created_by_email TEXT;

-- +goose Down
ALTER TABLE animal_registration_failures DROP COLUMN created_by_email;
ALTER TABLE animal_search_records DROP COLUMN created_by_email;
