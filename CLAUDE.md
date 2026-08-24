# go-apiserver — working notes

Go/Echo API server (`internal/server`) backing two clients: the Telangana
mobile app (`/api/mobile/v1`) and the Godhaar analytics web dashboard
(`/api/web/v1`, `internal/server/web/handlers/...`). Postgres via `sqlx`
(`internal/database/...`), migrations in `migrations/` (goose).

## Analytics dashboard latency: the `/analytics` query has zero supporting indexes

Reported live: the Godhaar analytics dashboard (`godhaar_analytics_dashboard`
repo) is slow. The frontend (`src/layouts/AnalyticsPanel.tsx`) was checked
first and ruled out — it's deliberately careful: a `draft`/`applied` filter
split so editing a filter never fires a request, `react-query` caching, no
polling, and the heavy `getAnalytics()` call only fires on an explicit
"Search" press (`enabled: applied !== null`), never on mount. The bottleneck
is server-side.

**`AdminAnalytics` (`internal/database/analytics/repository.go:52`), which
backs `GET /api/web/v1/analytics` — the dashboard's main Analytics tab —
filters and groups on columns that have never had an index, on either of the
two tables it touches:**

```sql
-- farmers subquery
WHERE ($1::text IS NULL OR state = $1)
  AND ($2::text IS NULL OR district = $2)
  AND ($3::text IS NULL OR mandal = $3)
  AND ($5::timestamptz IS NULL OR created_at >= $5)
  AND ($6::timestamptz IS NULL OR created_at <  $6)
GROUP BY created_by_email
-- animals subquery: same shape, plus a `breed` filter
```
...then a `FULL OUTER JOIN` of the two aggregated results on
`created_by_email`.

Checked the actual schema, not assumed: `migrations/20260719174400_create_farmers.sql`
and `20260719180622_create_animals.sql` only ever indexed `created_by`
(`idx_farmers_created_by`/`idx_animals_created_by`) and `farmer_id`
(`idx_animals_farmer_id`). `created_by_email` — a *different* column from
`created_by`, added later by
`migrations/20260802172155_add_created_updated_by_email_to_farmers.sql` /
`20260802172202_..._to_animals.sql` (a plain `ALTER TABLE ... ADD COLUMN`,
no index) — is the column `AdminAnalytics` **groups** by; the *filters* are
on `state`/`district`/`mandal`/`created_at` (+`breed` on `animals`). None of
those six columns (`state`, `district`, `mandal`, `breed`, `created_at`,
`created_by_email`) have ever had an index, on either table. Every dashboard search — any
filter combination, or none — forces a full sequential scan of both
`farmers` and `animals`, a hash aggregate on each, then the join. This gets
linearly worse as both tables grow, which is exactly a "used to be fine,
now it's slow" symptom rather than a constant-cost bug.

**This is a real gap, not a guess extrapolated from nothing:** every *other*
table in this schema has indexes matching its actual query patterns —
`animal_search_records` has `(created_by, created_at DESC)`,
`(decision, created_at DESC)`, a partial index on verified MATCHes, etc.
(`migrations/20260806171616_create_animal_search_records.sql`);
`animal_registration_failures` similarly
(`migrations/20260806171527_create_animal_registration_failures.sql`);
`cctv_video_analytics` has `(farmer_id, requested_at DESC)`. `farmers` and
`animals` — the two core tables the whole app is built on — are the outlier:
indexed only for their FK/ownership lookups (`created_by`, `farmer_id`),
never for this analytics access pattern.

Also checked and ruled out as contributing causes: no `SetMaxOpenConns`/
`SetMaxIdleConns`/etc. call anywhere in `internal/database/database.go` —
default Go `sql.DB` pool settings are in effect, not a likely bottleneck on
their own; and `GET /analytics/totals` (`AdminTotalAnalytics`, two bare
`COUNT(*)`s) is cached client-side for 15 minutes and called once per
session, so it's a much smaller contributor than the filtered/grouped
`AdminAnalytics` query re-run from scratch on every search.

**Not yet fixed — investigation only, as of this writing.** The fix is a
migration adding index(es) covering `farmers`/`animals`' actual filter/group
columns (`state`, `district`, `mandal`, `created_at`, `created_by_email` on
both, plus `breed` on `animals`) — not a code change. Two things worth
deciding before writing it, not guessing at:
- Composite index shape should match the real filter combinations the
  dashboard actually sends, not just index every column independently —
  worth checking `FilterOptions.tsx`/`toSearchFilters` for which
  combinations are actually reachable from the UI before choosing column
  order.
- The `($n::text IS NULL OR col = $n)` pattern used throughout this query is
  itself a known Postgres planner anti-pattern — a plain btree index on
  `col` is not guaranteed to be used for an `OR IS NULL` predicate the way
  it would be for a plain `col = $n`. Confirm with `EXPLAIN ANALYZE` against
  real data (both an unfiltered call and a filtered one) that a new index
  actually gets picked up, rather than assuming CREATE INDEX alone closes
  this — this exact class of "index exists but the planner doesn't use it"
  mistake is worth ruling out explicitly, not assumed away.
