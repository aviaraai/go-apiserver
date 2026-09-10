-- +goose Up
-- Persists per-candidate LightGlue re-rank evidence (see inference_server's
-- pipeline/rerank.py and scratch_rerank_eval.py's Stage 1 gate) for every
-- search, not just the top-1 verdict. Before this, captureSearch stored only
-- the decided candidate's score/gap/agreement -- no past search could be
-- diagnosed for how LightGlue's per-candidate evidence actually looked,
-- which is what makes future re-calibration (thresholds move once a
-- different candidate can occupy rank 1) possible at all.
--
-- Nullable JSONB, not a scalar column: production always has 1..searchTopK
-- candidates per search, each with its own embedding score, LightGlue
-- match_ratio/num_matches (nullable per candidate -- MUZZLE_CROP_CACHE_DIR
-- only covers animals registered after that cache shipped), and rank in
-- both the embedding-only and RRF-combined orderings. NULL when the
-- reranker didn't run at all (disabled, or fewer than 2 candidates).
ALTER TABLE animal_search_records
    ADD COLUMN lightglue_candidates JSONB;

-- +goose Down
ALTER TABLE animal_search_records
    DROP COLUMN lightglue_candidates;
