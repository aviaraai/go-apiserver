# CCTV upload feature — pre-merge verification runbook

Branch: `cctv-consolidated` (pushed to origin, not merged into `main`).
`go build ./...`, `go vet ./...`, and `go test ./...` all pass clean on this
branch already. What's left is a live-stack check against a real Postgres +
a real running `inference_server` — this doc is that checklist.

**Do not merge into `main` (or push a merge) until all 5 checks below pass.**

---

## What's in this branch

One commit on top of `origin/main` (`8c0cf26`): `73dc6c7 — Land the CCTV
upload feature from temp_cost_tag_feature, with two fixes`. Confirmed via
`git diff origin/main..cctv-consolidated --name-status` — exactly these 8
files, nothing else:

```
M  internal/database/cctv/repository.go
M  internal/server/routes.go
M  internal/server/web/handlers/cctv/errors.go
M  internal/server/web/handlers/cctv/routes.go
A  internal/server/web/handlers/cctv/routes_test.go
M  internal/server/web/handlers/cctv/schema.go
A  internal/server/web/handlers/cctv/upload.go
A  internal/server/web/handlers/cctv/upload_test.go
```

This branch was built by taking the CCTV feature's file *content* from
`temp_cost_tag_feature` (which mixes it with 24 unrelated files — tag_no/cost
changes, mobile endpoint changes, debug endpoints) and applying it fresh on
top of `origin/main`, so none of that unrelated work rides along. Two real
bugs were fixed on top before this commit:

1. **JSON-tag binding bug.** `AnalyseRequest.GoshalaPublicID` (in
   `internal/server/web/handlers/cctv/schema.go`) had only a `form:` tag, no
   `json:` tag. A JSON-body `POST /cctv/analyse` therefore never bound
   `goshala_public_id` at all — it silently came through empty. Fixed by
   giving the field both tags: `json:"goshala_public_id"
   form:"goshala_public_id"`. Regression test:
   `TestAnalyseJSONBodyBindsGoshalaID` in `routes_test.go`.

2. **Wrong camera source wired.** `internal/server/routes.go` had
   `cctv.NaiveImplementationSource{}` wired in — a source that serves a fixed
   local test file, not a real integration. Swapped to
   `cctv.NotImplementedSource{}`, which returns a clean `503` instead of
   silently serving fake data. Regression test:
   `TestAnalyseSourceUnavailable` in `routes_test.go`.

Also resolved while pulling this branch together: `internal/database/cctv/repository.go`
had `detail = :detail::jsonb` (a Postgres cast) in its named query. sqlx
cannot parse a `:name::type` cast — the `::` reads as a second bind
parameter, not a type cast — so this fails to compile as a query at runtime
on `main`'s original CCTV code path. This branch uses `detail = :detail`
(no cast) instead, confirmed correct via `TestNamedQueriesCompile`
(`internal/database/namedquery_test.go`, unrelated to this branch but
already in the repo) — an AST-based regex scan across every `.go` file
under `internal/` for the `:name::` pattern. **The same broken `::jsonb`/`::text`
cast pattern was independently found 4x in `internal/database/debug/repository.go`
on `main` itself** — a pre-existing, unrelated bug, not something this
branch introduced or needs to fix.

---

## What `secrets/.env` needs

None of these exist in the environment this branch was prepared in — no
`secrets/.env`, no reachable Postgres, no reachable `inference_server`. This
whole runbook is written assuming you're running it somewhere that has (or
can reach) real infra.

```
PORT=<port the go-apiserver should listen on>

DB_HOST=<postgres host>
DB_PORT=<postgres port>
DB_DATABASE=<database name>
DB_USER=<postgres user>
DB_PASSWORD=<postgres password>
DB_SCHEMA=<schema name>
SSL_MODE=<disable | require | verify-full, per your Postgres setup>

# Object storage — README says Cloudflare R2 (S3 SDK v2); config.go also
# still has GCS fields (GCS_BUCKET / GCS_CREDENTIALS_PATH) from an earlier
# storage backend. Use whichever this deployment actually uses.
ACCESS_KEY_ID=<R2 access key id>
ACCESS_KEY_SECRET=<R2 access key secret>
ACCOUNT_ID=<Cloudflare account id>
R2_BUCKET=<bucket name>
# — or, if still on GCS —
GCS_BUCKET=<bucket name>
GCS_CREDENTIALS_PATH=<path to a real GCS service-account JSON file>

SUPABASE_JWT_SECRET=<>=32 bytes — must match whatever issues your test/admin JWT>
SUPABASE_PROJECT_URL=<your Supabase project URL>
ADMIN_API_KEY=<>=32 bytes>

QR_ENCRYPTION_KEY=<32-byte value, hex-encoded>

INFERENCE_SERVER_URL=<base URL of a running inference_server, e.g. http://localhost:8000>
```

`internal/config/env.go` validates all of these at startup and fails fast
with a specific message per missing/malformed var — so if `go run
./cmd/api` complains, the error tells you exactly which one.

---

## Exact commands

```bash
# 1. Check out this branch
git fetch origin
git checkout cctv-consolidated

# 2. Fill in secrets/.env (see above), then run migrations
make migrate-up
make migrate-status   # sanity check — confirms it applied

# 3. Start the server
make run              # or: go run ./cmd/api
# Confirm it's up:
curl http://localhost:$PORT/health
```

Make sure a real `inference_server` process is running and reachable at
whatever `INFERENCE_SERVER_URL` points to before check 2 below — `POST
/cctv/analyse/upload` will call out to it for real.

### The 5 checks

**1. A real farmers row with `farmer_type='goshala'` exists.**
Check first; create one via whatever the normal farmer-registration path is
if none exists. The CCTV feature needs a real goshala to attach the
analysis session to.

**2. `POST /cctv/analyse/upload` — real video, multipart upload.**
Upload a real sample video. Confirm:
- The request blocks until the job completes and returns the expected
  response shape (not a bare job-id/polling response — this endpoint waits).
- The `total_animals` figure in the go-apiserver response matches what
  `inference_server`'s own `/cctv/jobs/{id}/result` reports for that same
  video (hit `inference_server` directly with the same clip, or read back
  its `/result` for the job id this request produced, and compare).

**3. `POST /cctv/analyse` (JSON body, no upload) — now `503`.**
Before this branch: `400` (silent binding failure — bug #1 above).
After: `503` with a body naming `CCTV_SOURCE_UNAVAILABLE` (from
`NotImplementedSource`, bug #2 above). Confirm you get the 503, not a 400
and not a 200.

**4. `GET /cctv/requests` — the check-2 session shows up.**
Confirm the session created by check 2 appears in the history list, with
sane fields (goshala id, counts, timestamp).

**5. (Implicit — covered by checks 2-4 succeeding at all) DB writes,
migrations, and the `detail`/`::jsonb` query actually work against a real
Postgres**, not just against the unit tests' mocked repository.

---

## Only merge into `main` (and push) once all 5 checks pass

If any check fails, don't merge — report back which check failed and what
the server returned (status code + body), so it can be fixed on
`cctv-consolidated` and re-verified before another attempt.
