-- +goose Up
CREATE TABLE animal_search_records (
    id                  BIGSERIAL PRIMARY KEY,
    decision            TEXT NOT NULL CHECK (decision IN ('MATCH', 'REVIEW', 'UNKNOWN', 'FAILED')),
    godhaar_id          TEXT,
    score               DOUBLE PRECISION,
    error_code          TEXT,
    verified            TEXT NOT NULL DEFAULT 'not_verified' CHECK (verified IN ('yes', 'no', 'not_verified')),
    image_keys          JSONB NOT NULL DEFAULT '[]',
    app_version         TEXT,
    os_version          TEXT,
    device_model        TEXT,
    device_manufacturer TEXT,
    detail              JSONB NOT NULL DEFAULT '{}',
    created_by          TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (decision = 'MATCH' AND godhaar_id IS NOT NULL)
        OR (decision <> 'MATCH' AND godhaar_id IS NULL)
    ),
    CHECK (
        decision = 'MATCH'
        OR verified = 'not_verified'
    ),
    CHECK (
        (decision = 'FAILED' AND error_code IS NOT NULL AND score IS NULL)
        OR (decision <> 'FAILED' AND error_code IS NULL)
    )
);
CREATE INDEX idx_asr_created_at_id ON animal_search_records (created_at DESC, id DESC);
CREATE INDEX idx_asr_decision_created ON animal_search_records (decision, created_at DESC);
CREATE INDEX idx_asr_verified_created ON animal_search_records (verified, created_at DESC) WHERE decision = 'MATCH';
CREATE INDEX idx_asr_godhaar_created ON animal_search_records (godhaar_id, created_at DESC) WHERE godhaar_id IS NOT NULL;
CREATE INDEX idx_asr_created_by_created_at ON animal_search_records (created_by, created_at DESC);

-- +goose Down
DROP TABLE animal_search_records;
