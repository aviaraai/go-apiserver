# go-api-server

A Go HTTP API for a livestock registry ("Godhaar"). It manages **farmers**, their **animals**, the **images** captured for each animal, generated **QR codes**, and aggregate **analytics**. Data lives in PostgreSQL; uploaded files (photos, QR images) live in Cloudflare R2 object storage.

The codebase follows a consistent layered structure: every business domain has an HTTP layer (under `internal/server/handlers`) and a matching data-access layer (under `internal/database`). Understanding one domain means you understand them all.

---

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go 1.26 |
| HTTP framework | [Echo v4](https://echo.labstack.com/) |
| Database | PostgreSQL, accessed via `pgx` (driver) and `sqlx` (query helpers) |
| Object storage | Cloudflare R2, via the AWS S3 SDK v2 |
| Config | Environment variables, auto-loaded from `.env` (`godotenv`) |
| QR codes | `skip2/go-qrcode` |

---

## Folder structure

```
go-api-server/
├── cmd/
│   └── api/                     Application entrypoint (main package)
├── internal/                    All private application code
│   ├── server/                  HTTP server bootstrap, routing, middleware
│   │   └── handlers/            HTTP layer — one sub-package per domain
│   │       ├── analytics/
│   │       ├── animal/
│   │       ├── farmer/
│   │       ├── images/
│   │       └── qr/
│   ├── database/                Data-access layer — one sub-package per domain
│   │   ├── analytics/
│   │   ├── animal/
│   │   ├── farmer/
│   │   ├── images/
│   │   ├── qr/
│   │   └── database.go          Shared DB connection + health check
│   ├── storage/                 Object-storage abstraction (Cloudflare R2)
│   ├── imaging/                 Image validation & content-type helpers
│   ├── aesgcm/                  AES-GCM encrypt/decrypt helpers
│   └── id/                      Public ID generators (farmer / animal)
├── .env                         Local configuration (keep private)
├── makefile                     Common build/run/test commands
├── go.mod / go.sum              Module definition and dependency lockfile
└── main.go                      Standalone scratch script (see note below)
```

### `cmd/api/` — entrypoint

The real program starts here. It builds the server, starts listening, and handles graceful shutdown on `SIGINT`/`SIGTERM`. This is what you run in production.

### `internal/server/` — HTTP bootstrap

Owns the Echo instance and everything shared across the API:

- Creates the server, reads the `PORT`, and wires up the database connection.
- Registers global middleware (request logging, panic recovery).
- Installs a central error handler that turns errors into a consistent JSON `{"message": ...}` shape.
- Exposes a `/health` endpoint backed by the database health check.

Each domain's routes are registered here (or intended to be) by calling that domain's `RegisterRoutes` function.

### `internal/server/handlers/<domain>/` — the HTTP layer

Every domain sub-package looks the same and has a clear job: **translate HTTP ↔ business calls**. It does *not* contain SQL. A typical package holds two files:

- **`routes.go`** — defines:
  - A `Handler` struct holding its dependencies (a `Repository` and `Storage`).
  - A local `Repository` **interface** listing only the data-access methods this handler needs. The concrete implementation lives in `internal/database/<domain>`, so the HTTP layer depends on an interface, not on the database package directly.
  - `RegisterRoutes(...)` mapping URL paths to handler methods.
  - The handler methods themselves: bind/validate the request, call the repository and/or storage, map domain errors to HTTP status codes, and return JSON.
- **`schema.go`** — the request and response structs (DTOs) with their `json`/`param`/`query` tags. These define the API's public contract and are deliberately separate from the database models.

Domains: `farmer`, `animal`, `images`, `qr`, `analytics`.

### `internal/database/<domain>/` — the data-access layer

The counterpart to each handler package. It owns all SQL and knows nothing about HTTP. A typical package holds:

- **`repository.go`** — a `Repository` struct wrapping a `*sqlx.DB`, plus `NewRepository(...)`. Its methods run the actual queries and return domain models. This is what satisfies the handler's `Repository` interface.
- **`models.go`** — structs with `db:"..."` tags that map to table rows / query parameters. These are the internal database shapes, kept separate from the HTTP DTOs.
- **`errors.go`** (where present) — sentinel errors (e.g. "not found", "duplicate") and a helper that translates raw PostgreSQL error codes into those sentinels, so handlers can branch on meaning rather than on driver internals.

### `internal/database/database.go` — shared connection

Not tied to a domain. Builds the PostgreSQL connection string from environment variables, opens a pooled connection, and exposes `Health()` (connection-pool stats for `/health`) and `Close()`.

### `internal/storage/` — object storage

An interface-based abstraction over Cloudflare R2 (via the S3 SDK). Provides `Upload(...)` (store a file from an `io.Reader`) and `PresignedURL(...)` (generate a temporary download link). Handlers depend on the `Storage` interface, so the backend could be swapped without touching them.

### `internal/imaging/` — image validation

Utilities to sniff and validate uploaded images: confirms the bytes are a real, supported image (JPEG/PNG), enforces dimension/pixel limits, and maps content types to file extensions. Used by handlers before anything is uploaded to storage.

### `internal/aesgcm/` — encryption helpers

Small `Encrypt` / `Decrypt` helpers using AES-GCM. Used to encode data (e.g. an animal's ID) into QR-code content that can be verified later.

### `internal/id/` — ID generation

Generators for the public-facing farmer and animal ("Godhaar") IDs used across the API.

---

## How a request flows

Using "register a farmer" as the example — every domain follows this same path:

```
HTTP request
   │
   ▼
internal/server/handlers/farmer/routes.go   ← bind & validate request (schema.go DTOs)
   │                                            validate photo (internal/imaging)
   │                                            upload photo (internal/storage → R2)
   ▼
Repository interface (defined in the handler package)
   │
   ▼
internal/database/farmer/repository.go      ← run SQL via sqlx (models.go structs)
   │                                            translate DB errors (errors.go)
   ▼
PostgreSQL
   │
   ▼
handler maps result → response DTO → JSON
```

The key design idea is the **interface seam** between the two layers: handlers declare *what* data operations they need as an interface, and the `database/<domain>` packages provide the *how*. This keeps HTTP concerns and SQL concerns cleanly separated and makes handlers easy to test with a fake repository.

---

## Configuration

Configuration is read from environment variables (loaded automatically from a `.env` file at startup). The variables the app expects:

| Variable | Purpose |
|---|---|
| `PORT` | Port the HTTP server listens on |
| `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD`, `DB_SCHEMA` | PostgreSQL connection |
| `ACCESS_KEY_ID`, `ACCESS_KEY_SECRET`, `ACCOUNT_ID` | Cloudflare R2 credentials (used by `internal/storage`) |

> **Note:** never commit real secrets. Treat the local `.env` as private and use per-environment configuration in deployment.

---

## Running locally

Common tasks are wrapped in the `makefile`:

```bash
make build        # build the application
make run          # run the API server
make test         # run the unit test suite
make itest        # run DB integration tests (spins up a Postgres container)
make docker-run   # start a local Postgres container
make docker-down  # stop the local Postgres container
make watch        # run with live reload
make all          # build + test
make clean        # remove the last build's binary
```

Without `make`, you can run the server directly with `go run ./cmd/api` and the tests with `go test ./...`.

The server exposes `GET /health` for a quick liveness/database check.

---

## Notes on repository state

A few things to be aware of when navigating the code:

- **`main.go` at the repository root is a standalone scratch script** (it writes a sample QR image to disk) and is unrelated to the API. The actual service entrypoint is **`cmd/api/`**.
- Some pieces are still stubs or in progress — for example the `internal/id` generators and a few handler methods — so not every route is fully wired end-to-end yet. The structure above describes the intended, consistent shape that each domain follows.
