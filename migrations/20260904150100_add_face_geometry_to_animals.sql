-- +goose Up
-- Supplementary, return-only signal from the inference server's pose model
-- (pipeline/pose.py -> pipeline/geometry.py in the inference_server repo).
-- Nullable: absent on every animal registered before this column existed,
-- and on any registration where the pose model didn't load or found no
-- usable face (status NO_FACE/NO_RULER — every ratio is null inside the
-- JSON itself in that case too). Nothing in this server reads or decides on
-- it; it is stored so a future analysis/comparison step has real data to
-- work from instead of none.
ALTER TABLE animals ADD COLUMN face_geometry JSONB;

-- +goose Down
ALTER TABLE animals DROP COLUMN face_geometry;
