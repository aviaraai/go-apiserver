-- +goose Up
ALTER TABLE animal_registration_failures
    ADD COLUMN registration_id UUID NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE animal_search_records
    ADD COLUMN search_id UUID NOT NULL DEFAULT gen_random_uuid();

CREATE UNIQUE INDEX idx_arf_registration_id ON animal_registration_failures (registration_id);
CREATE UNIQUE INDEX idx_asr_search_id ON animal_search_records (search_id);

-- +goose Down
DROP INDEX idx_asr_search_id;
DROP INDEX idx_arf_registration_id;
ALTER TABLE animal_search_records DROP COLUMN search_id;
ALTER TABLE animal_registration_failures DROP COLUMN registration_id;
