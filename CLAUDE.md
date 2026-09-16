# go-apiserver — codebase notes

Go/Echo API server (`internal/server`) backing two clients: the Telangana
mobile app (`/api/mobile/v1`) and the Godhaar analytics web dashboard
(`/api/web/v1`, `internal/server/web/handlers/...`). Postgres via `sqlx`
(`internal/database/...`), migrations in `migrations/` (goose).

## Muzzle search path

`internal/server/mobile/handlers/animal/routes.go` calls inference_server,
then aggregates: **max across each animal's stored embeddings** (`cattleScores`,
~line 498). inference_server already took the **median across the N query
photos**. That pair is what the thresholds were calibrated on; changing either
side invalidates them.

`decide()` in `decision.go` produces the verdict the app reads. inference_server
now returns its own decision in `inference_decision`; it is **shadow-logged
only** and does not drive the response. `translate.go::translateDecision()`
resolves `faiss_id` → `godhaar_id` — MATCH with an unmapped id degrades to
UNKNOWN, REVIEW keeps whatever resolves.

`calibratedMuzzlePhotoCount = 3`; fewer photos sets `CalibrationWarning`.

## Inference error contract

`internal/inference/errors.go` (`domainCodes` / `domainResponses` /
`perImageMessages`) and inference_server's `schema.ErrorCode` are **one
contract and must change together** — adding a code is a two-repo change. A 4xx
body from inference without an `error_code` is treated here as a contract fault,
not a verdict, and never surfaces to the officer as one.

## Analytics dashboard latency (open, investigation only)

`AdminAnalytics` (`internal/database/analytics/repository.go:52`), behind
`GET /api/web/v1/analytics`, filters on `state`/`district`/`mandal`/`created_at`
(+`breed` on `animals`) and groups on `created_by_email`. **None of those six
columns has an index on either table.** `farmers`/`animals` were only ever
indexed for FK/ownership lookups (`created_by`, `farmer_id`);
`created_by_email` was added later by a bare `ALTER TABLE ... ADD COLUMN`.
Every dashboard search sequentially scans both tables, hash-aggregates each,
then `FULL OUTER JOIN`s — cost grows with table size, which matches the
"used to be fine" symptom.

Every other table here is indexed for its real access pattern
(`animal_search_records`, `animal_registration_failures`,
`cctv_video_analytics`), so this is a gap, not the house style.

Ruled out: the frontend (`AnalyticsPanel.tsx` only fires on an explicit Search
press, `enabled: applied !== null`, with react-query caching and no polling);
`/analytics/totals` (two bare `COUNT(*)`s, cached 15 min, once per session);
connection pool (defaults, no `SetMaxOpenConns` anywhere, unlikely alone).

Before writing the migration, two things to settle rather than guess:
- Composite column order should match the filter combinations the UI can
  actually reach (`FilterOptions.tsx` / `toSearchFilters`), not one index per
  column.
- The `($n::text IS NULL OR col = $n)` pattern is a known planner
  anti-pattern — a plain btree index is not guaranteed to be used for
  `OR IS NULL`. Confirm with `EXPLAIN ANALYZE` (filtered and unfiltered) that
  a new index is actually picked up.
