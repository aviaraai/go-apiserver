-- +goose Up
CREATE TABLE cctv_video_analytics (
    id                  BIGSERIAL PRIMARY KEY,
    farmer_id           BIGINT NOT NULL REFERENCES farmers(id),
    status              TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    inference_job_id    TEXT,
    source_video_key    TEXT,
    annotated_video_key TEXT,
    total_animals       INTEGER,
    total_clear_animals INTEGER,
    error_code          TEXT,
    detail              JSONB NOT NULL DEFAULT '{}',
    requested_by        TEXT NOT NULL,
    requested_by_email  TEXT NOT NULL,
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at        TIMESTAMPTZ,

    CONSTRAINT chk_cctv_succeeded_has_result CHECK (
        status <> 'succeeded'
        OR (total_clear_animals IS NOT NULL
            AND total_animals IS NOT NULL
            AND annotated_video_key IS NOT NULL
            AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_cctv_failed_has_code CHECK (
        status <> 'failed' OR (error_code IS NOT NULL AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_cctv_running_is_open CHECK (
        status <> 'running' OR completed_at IS NULL
    )
);
CREATE INDEX idx_cctv_created_at_id ON cctv_video_analytics (requested_at DESC, id DESC);
CREATE INDEX idx_cctv_farmer_requested ON cctv_video_analytics (farmer_id, requested_at DESC);
CREATE INDEX idx_cctv_status_requested ON cctv_video_analytics (status, requested_at DESC);
CREATE INDEX idx_cctv_requested_by ON cctv_video_analytics (requested_by, requested_at DESC);

-- +goose Down
DROP TABLE cctv_video_analytics;
