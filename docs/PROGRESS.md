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

## Stage 2 — wallet + ledger — DONE (2026-07-13)

Built:
- `backend/migrations/000002_wallet_ledger_schema.{up,down}.sql`:
  - `accounts` — `id`, `owner_user_id` (nullable FK to `users`, NULL for system accounts),
    `type` enum (`commuter_wallet`, `driver_earnings`, `owner_revenue`, `platform_fee`,
    `funding_source`), `created_at`. Partial unique indexes enforce at most one account per
    `(owner_user_id, type)` and at most one system account per `type`.
  - `ledger_transactions` — `id`, `kind` enum (`topup`, `fare`), `idempotency_key` (nullable,
    unique), `created_at`, `metadata` jsonb.
  - `ledger_postings` — `id`, `transaction_id` FK, `account_id` FK, `amount_cents` (signed
    int64), `created_at`.
  - **Sign convention**: `amount_cents` is signed — negative = debit (money leaving an
    account), positive = credit (money entering one).
  - **Balance invariant enforced in the DB, not just in Go**: a `DEFERRABLE INITIALLY
    DEFERRED` constraint trigger (`ledger_postings_balanced`) fires per posting row-change and
    checks that all postings for that `transaction_id` sum to zero — checked once at `COMMIT`,
    after every posting in a transaction has been inserted.
  - Account balances are never stored — always `SUM(amount_cents)` over `ledger_postings`, so
    there's no balance column to drift out of sync.
- `backend/internal/wallet/` — the wallet module:
  - `models.go`, `repo.go`, `handlers.go`. A `querier` interface (satisfied by both
    `*pgxpool.Pool` and `pgx.Tx`) lets repo helpers (account get-or-create, balance lookup)
    run either standalone or inside a caller-managed transaction.
  - `Repo.Topup` — simulated top-up (no real payment gateway — commented in code), moves
    `amount_cents` from `funding_source` into the caller's `commuter_wallet`, all in one DB
    transaction.
  - `Repo.ChargeFare` — the correctness-critical path, all in one DB transaction:
    1. Inserts the `ledger_transactions` row with `ON CONFLICT (idempotency_key) DO NOTHING
       RETURNING ...`. If the insert is a no-op (key already used), fetches and returns the
       existing transaction with **no new postings** — true idempotency, including under
       concurrent replay (the second inserter blocks on the unique index until the first
       commits, then correctly sees the conflict).
    2. Resolves `vehicle_id` → owner + active driver via `vehicles` ⋈ `vehicle_assignments`
       (`active = true`) ⋈ `drivers`, reusing Stage 1's tables directly rather than
       duplicating owner/driver lookups.
    3. Takes `SELECT ... FOR UPDATE` on the commuter's account row before reading its balance
       — this is what serializes two concurrent charges against the same wallet, since the
       second charge's lock acquisition blocks until the first transaction commits or rolls
       back.
    4. Rejects with `ErrInsufficientFunds` if balance < fare, rolling back with no postings
       made.
    5. Splits the fare (see below) and posts four rows: commuter debit, driver credit, owner
       credit, platform credit — the deferred trigger confirms they sum to zero at commit.
  - Endpoints wired into `internal/server/router.go`, all behind `identity.RequireAuth`:
    - `POST /wallet/topup` (commuter only) — `{amount_cents}` → `{transaction_id,
      balance_cents}`.
    - `GET /wallet/balance` (any authenticated role) — reports the balance of the account
      matching the caller's role (`commuter_wallet` / `driver_earnings` / `owner_revenue`),
      lazily creating that account on first read.
    - `POST /fare/charge` (driver only) — `{commuter_id, vehicle_id, fare_cents,
      idempotency_key}` → `{transaction_id, replayed, fare_cents, platform_cents,
      driver_cents, owner_cents}`. 402 on insufficient funds, 422 if the vehicle has no active
      driver assignment, 400 if `idempotency_key` is missing.
- `backend/internal/config` — added `FareSplit{PlatformPct, DriverPct, OwnerPct}`, defaults
  **10 / 25 / 65**. Platform and driver shares are rounded down (`fare_cents *
  pct / 100`); owner's share is whatever remains, so the three always sum to exactly
  `fare_cents` with no remainder lost or invented.
- `backend/cmd/server/main.go` — calls `walletRepo.EnsureSystemAccounts` once at startup
  (same "warn and continue" pattern as migrations if the DB isn't reachable yet).
- `backend/cmd/seed/main.go` — seeds `funding_source`/`platform_fee` system accounts, then
  gives each seeded commuter a starting balance (R100 / 10000 cents) via a real `Topup`
  transaction rather than a raw balance write. Re-running is a no-op: it checks the
  commuter's current balance first and only tops up if it's zero (a top-up has no
  idempotency key to dedupe on, so the balance check is what keeps re-seeding safe).
- Tests (`backend/internal/wallet/ledger_test.go`, against a real Postgres, skips like the
  Stage 0/1 integration tests if none is reachable):
  - `TestTopupThenBalance` — sanity check of the happy path.
  - `TestSplitSumsToFare` — split sums to exactly `fare_cents` across a range of amounts,
    including several that don't divide evenly by 10/25/65.
  - `TestLedgerInvariant` — a fare transaction's postings sum to zero.
  - `TestIdempotentFareCharge` — same `idempotency_key` charged twice → one transaction,
    balance debited exactly once, second call reports `replayed: true`.
  - `TestInsufficientFundsRejected` — charge exceeding balance is rejected, balance
    unchanged.
  - `TestConcurrentChargesOnlyOneSucceeds` — two goroutines fire concurrent charges against a
    wallet that can only afford one; exactly one succeeds, the other gets
    `ErrInsufficientFunds`, and the final balance is correct (never negative).

Decisions / deviations from the original plan:
- The stage brief said fare charge takes `vehicle_id/driver_id`; I chose **`vehicle_id`**
  only (not a separate `driver_id`), and derive both the driver and the owner from the
  vehicle's active `vehicle_assignments` row. This reuses Stage 1's assignment data instead
  of trusting a client-supplied driver id, and matches the real boarding flow (a driver scans
  within the context of the vehicle they're currently assigned to).
- `idempotency_key` is required (400 if missing) rather than optional for `/fare/charge` —
  the stage brief's safety guarantees only make sense if every charge carries one.
- The balance-sums-to-zero invariant is enforced with a `DEFERRABLE INITIALLY DEFERRED`
  constraint trigger rather than a plain `CHECK` constraint, since Postgres `CHECK` can't see
  other rows (needed to sum sibling postings) and a non-deferred trigger would fail on the
  first of several postings inserted per transaction, before the rest arrive.
- Concurrency safety uses `SELECT ... FOR UPDATE` on the `accounts` row as a lock primitive,
  even though the row has no balance column — Postgres still blocks concurrent lockers on
  that row, which is enough to serialize charges per-wallet without adding a separate lock
  table.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .` (clean for wallet code), and
`go mod tidy` all pass. Full test suite (including the wallet integration/concurrency tests)
run and passed against a disposable Postgres container. Also verified end-to-end by hand:
seeded, started the server, and exercised `POST /auth/login` → `POST /wallet/topup` →
`GET /wallet/balance` → `POST /fare/charge` → replayed `POST /fare/charge` with the same
`idempotency_key` (same `transaction_id` returned, `replayed: true`, balance debited exactly
once) → `GET /wallet/balance`.

---

Next: Stage 3 — routing
