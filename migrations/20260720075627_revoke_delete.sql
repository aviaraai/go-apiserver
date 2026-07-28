-- +goose Up
REVOKE ALL ON farmers, animals, qr, images, embeddings FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON farmers, animals, qr, images, embeddings TO postgres;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA godhaar_schema TO postgres;

-- +goose Down
GRANT ALL ON farmers, animals, qr, images, embeddings TO PUBLIC;
REVOKE ALL ON farmers, animals, qr, images, embeddings FROM postgres;
