# Ses'fikile — build log

Running record of what's actually been built, stage by stage. Read this first to see
current state before starting new work.

## Stage 0 — scaffold + infra — DONE (2026-07-13)

Built:
- Monorepo skeleton: `apps/{commuter,driver,owner}`, `packages/shared`, `docs` (placeholders only).
- `infra/docker-compose.yml` — Postgres 16 service, container `sesfikile-postgres`, user/pass/db
  `sesfikile`/`sesfikile_dev`/`sesfikile`, port 5432, named volume for persistence, `pg_isready`
  healthcheck.
- `backend/` Go module (`sesfikile/backend`):
  - `internal/config` — loads `PORT` (default 8080) and `DATABASE_URL` (default
    `postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable`) from env.
  - `internal/db` — pgxpool wrapper + `Ping(ctx)`. Pool creation is lazy (no eager connect),
    so the server starts cleanly even if Postgres is down.
  - `internal/server` — chi router + `GET /health`, pings the DB: 200
    `{"status":"ok","db":"ok"}` if reachable, 503 `{"status":"degraded","db":"down"}` if not.
  - `cmd/server/main.go` — wires it together, graceful shutdown via `signal.NotifyContext`
    (SIGINT/SIGTERM).
  - `health_test.go` — unit test for the handler using a fake DB pinger, covering both the
    healthy and degraded branches without needing a live Postgres.

Decisions / deviations from the original plan:
- `go.mod`'s `go` directive ended up at `1.25.0` after `go mod tidy` pulled in a dependency
  requiring it, rather than the originally planned `1.22`. Still satisfies the "Go 1.22+"
  requirement.
- No app Dockerfile / containerized backend yet — only Postgres runs in Docker for Stage 0;
  the Go binary runs via `go run`/`go build` on the host for faster iteration. This matches
  the original plan (infra-only Compose, no app service).

Verified: Postgres container builds and reports healthy, `go build ./...` and `go test ./...`
pass, and `/health` returns `ok` against a live DB and `degraded` when the DB is down (verified
end-to-end using a disposable container on an alternate port, since this dev machine also runs
a native Windows PostgreSQL 18 service that otherwise shadows port 5432 — see CLAUDE.md Stage 0
note).

---

## Stage 1 — identity — DONE (2026-07-13)

Built:
- `backend/migrations/000001_identity_schema.{up,down}.sql` — golang-migrate SQL migrations,
  embedded into the binary via `backend/migrations/embed.go` (`//go:embed *.sql`) and applied
  automatically by `internal/db.Migrate` (`internal/db/migrate.go`) on every `cmd/server` and
  `cmd/seed` startup. Enables `pgcrypto` for `gen_random_uuid()`. Tables (uuid PKs,
  `created_at`/`updated_at` on all):
  - `users` — `phone` (unique), `email` (nullable unique), `password_hash`, `role` (enum:
    `commuter`/`driver`/`owner`).
  - `drivers` — FK `user_id` (unique — one driver profile per user), `full_name`,
    `prdp_number`, `prdp_verified` (bool, default false), `id_number`, `kyc_status` (enum:
    `pending`/`verified`/`rejected`, default `pending`).
  - `vehicles` — FK `owner_user_id`, `registration` (unique), `capacity`, `association_name`,
    `compliance_status` (enum: `pending`/`verified`, default `pending`).
  - `vehicle_assignments` — FK `vehicle_id`, FK `driver_id`, `active` bool. Partial unique
    indexes on `vehicle_id`/`driver_id` where `active` enforce at most one active assignment
    per vehicle and per driver.
  - Owners and commuters are plain `users` rows with the matching `role` — no separate
    profile tables, per the stage scope.
- `backend/internal/identity/` — the identity module:
  - `models.go`, `password.go` (bcrypt hash/verify), `jwt.go` (HS256 issue/parse via
    `golang-jwt/jwt/v5`, 24h expiry, claims carry user id + role), `repo.go` (pgx queries),
    `handlers.go`, `middleware.go` (`RequireAuth`, `RequireRole`).
  - Endpoints wired into the existing chi router (`internal/server/router.go`):
    `POST /auth/register`, `POST /auth/login` (both public), `GET /me` (protected, returns
    the caller's user id + role from the validated JWT — the one protected test route called
    for by the stage brief).
  - `prdp_verified` and `kyc_status` are stored-only fields with no verification workflow
    wired up — flagged in both the migration and `models.go` per CLAUDE.md "SCOPE HONESTY".
- `backend/internal/config` — added `JWTSecret`, loaded from `JWT_SECRET` env var with a
  dev-only fallback (documented in `.env.example`).
- `backend/cmd/seed/main.go` — seeds 1 owner, 2 vehicles, 2 drivers (each assigned to a
  vehicle), and 2 commuters with known dev passwords; re-running is a no-op for rows that
  already exist (matched by unique constraints) and prints the seeded logins.
- Tests: `password_test.go`, `jwt_test.go` (issue/parse, wrong secret, expired token),
  `middleware_test.go` (`RequireAuth`/`RequireRole` allow/block), and
  `integration_test.go` (register → login → `/me` against a real Postgres — skips instead of
  failing if no DB is reachable, matching the Stage 0 health-check test's approach).
  `internal/server/health_test.go` updated to build a router through the new
  `NewRouter(pinger, identityHandlers, tokens)` signature.

Decisions / deviations from the original plan:
- Migrations are embedded (`go:embed`) and run automatically from Go code rather than via a
  separate Makefile/shell script, since the stage brief allows either — this keeps `cmd/seed`
  and `cmd/server` both self-migrating through a single code path. The raw `golang-migrate`
  CLI commands are documented in `.env.example` for anyone who wants to run migrations by
  hand.
- `vehicle_assignments` gets partial unique indexes (one active assignment per vehicle/driver)
  rather than a plain boolean-only column — this is a real data invariant the stage brief
  implies ("a driver assigned to a vehicle") and costs nothing extra to enforce at the DB
  layer.

Verified: `go build ./...`, `go vet ./...`, and `go test ./...` all pass. End-to-end verified
against a disposable Postgres container on an alternate port (same reason as Stage 0 — the
native Windows Postgres service shadows 5432): ran migrations, seeded dev data, started
`cmd/server`, and curled `POST /auth/login` → `GET /me` (200 with correct user id/role) and
`GET /me` with no token (401).

---

Next: Stage 2 — wallet + ledger
