-- +goose Up
CREATE TABLE embeddings (
    id                 BIGSERIAL PRIMARY KEY,
    animal_id          BIGINT NOT NULL REFERENCES animals(id) ON DELETE RESTRICT,
    embedding_type TEXT NOT NULL CHECK (embedding_type = 'muzzle'),
    sequence INT NOT NULL DEFAULT 1,
    faiss_id BIGINT NOT NULL UNIQUE,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(animal_id, embedding_type, sequence)
);
CREATE INDEX idx_embeddings_animal_id ON embeddings(animal_id);

-- +goose Down
DROP TABLE embeddings;
