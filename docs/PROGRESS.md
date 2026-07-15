# Ses'fikile — build log

Running record of what's actually been built, stage by stage. Read this first to see
current state before starting new work.

## Baseline verified — clean-slate build green (2026-07-14)

Full clean-slate verification, not new work — confirms the state below is reproducible
from nothing, not an artifact of an accumulated dev environment.

**Current state**: backend feature-complete through Stage 8 (identity, wallet+ledger,
routing, telemetry, boarding, request-a-stop, fuel (mock), owner analytics), plus the
backend-cleanup pass (`/stops`, commuter transaction history, list `[]` serialization),
the test-data-isolation housekeeping pass (`cmd/cleanup`), and the fuel anti-bypass
test-isolation fix. Three frontend apps: `apps/driver` (9a), `apps/commuter` (9b-i live
map/search + 9b-ii wallet/boarding-pass), `apps/owner` (9c dashboard) — all built against
the Stage 0-8 API surface with no backend changes since. Stage 9d (cross-cutting polish)
not started; the MVP feature set is otherwise complete.

**Verified this pass**: fresh Postgres (`docker compose down -v` then `up` — a genuinely
empty volume, not a reset schema), migrations + `cmd/seed` run from empty — produced
exactly the 8 real Cape Town corridors and 12 real stops (3 interchanges: Athlone, Cape
Town Station, Wynberg), no leftover test junk. `go test -race -count=1 ./...` green in
every package, **no `-p 1` needed** — the suite is race-clean and parallel-safe. All
three apps: `npm install` + `npx tsc --noEmit` + `npm run build` all succeed clean.

**Non-blocking, deferred (noted for honesty, not acted on)**:
- `npm audit` reports 2 advisories in transitive dev dependencies. Not running
  `audit fix --force` — that command rewrites direct dependency versions to satisfy
  transitive advisories and has a history of silently introducing breaking changes;
  not worth the risk for dev-only transitive findings at this stage.
- `apps/owner`'s bundle exceeds Vite's 500kB chunk-size advisory (Recharts), and
  `apps/driver`'s bundle is heavy (html5-qrcode). Both build and run correctly as-is;
  code-splitting is a candidate only if/when deploying over real mobile networks, not
  before.

### Stand this up from scratch (PowerShell)

```powershell
# 1. Fresh Postgres (drops any existing volume — genuinely empty)
cd infra
docker compose down -v
docker compose up -d
cd ..

# 2. Backend: migrate + seed from empty
cd backend
$env:DATABASE_URL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"
go run ./cmd/seed        # applies migrations, then seeds the 8 real corridors

# 3. Backend test suite (race-clean, parallel-safe — no -p 1 needed)
go test -race -count=1 ./...

# 4. Backend server (separate terminal, for frontend dev against it)
go run ./cmd/server

# 5. Each frontend app (separate terminals; ports 5174/5175/5176)
cd ..\apps\driver;   npm install; npx tsc --noEmit; npm run build; npm run dev
cd ..\apps\commuter; npm install; npx tsc --noEmit; npm run build; npm run dev
cd ..\apps\owner;    npm install; npx tsc --noEmit; npm run build; npm run dev
```

---

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

---

## Stage 3 — routing — DONE (2026-07-13)

Built:
- `backend/migrations/000003_routing_schema.{up,down}.sql`:
  - `stops` — `id`, `name`, `latitude`/`longitude` (float8), `created_at`.
  - `routes` — `id`, `name`, `association_name`, `created_at`.
  - `route_legs` — `id`, `route_id` FK, `from_stop_id`/`to_stop_id` FK, `sequence` int,
    `fare_cents` int64, `created_at`. `UNIQUE (route_id, sequence)`.
  - **SCOPE HONESTY** (per CLAUDE.md): the migration and seed data are both commented as a
    hand-seeded, representative sample of Cape Town taxi corridors for demo purposes — NOT
    association-approved or authoritative. Real association routing sign-off is an open
    dependency.
- `backend/internal/routing/` — the routing module:
  - `models.go`, `repo.go` (plain CRUD/list queries; `AllRoutesWithLegs` loads every route +
    ordered legs in one call — the seeded dataset is small enough to search entirely in Go
    rather than express the path search as SQL).
  - `graph.go` — the pure, DB-free search algorithm (`Search(routes, origin, dest)`):
    - A route is only walkable in **increasing `sequence` order** — it models a real minibus
      taxi corridor that runs in one fixed direction, not a bidirectional graph edge. Asking
      for the reverse direction correctly finds no path.
    - **Path-selection ordering: fewest transfers first, then lowest fare.** Direct (0
      transfers) is always checked and preferred over any transfer path, even if a transfer
      path would be cheaper. Among 0-transfer candidates (multiple routes both containing
      origin and dest), the lowest-fare one wins; same for 1-transfer candidates.
    - **Supports at most one transfer** (one interchange stop), per the stage brief — this is
      a deliberate scope limit, not a general shortest-path implementation. A 2+ transfer
      itinerary will report no path even if one theoretically exists.
    - No path → `Search` returns `ok=false`; the handler turns this into a 404 with a JSON
      error body, not a 500.
  - `handlers.go`:
    - `GET /routes` — list of routes (id, name, association_name).
    - `GET /routes/{id}` — a route's ordered legs, each annotated with from/to stop names
      (looked up in one extra query) — useful for rendering a route on the commuter map later.
    - `GET /routes/search?from=<stop id or name>&to=<stop id or name>` — accepts either a
      stop UUID or an exact stop name for `from`/`to` (kept simple, no fuzzy matching).
      Returns `{transfers, total_fare_cents, segments: [{route_id, route_name, legs, fare_cents}]}`.
      404 with an error body if no path exists.
  - None of these routes require auth — route/fare data is public reference data, unlike
    wallet/fare endpoints.
- `backend/internal/server/router.go`, `backend/cmd/server/main.go` — wired
  `routing.NewRepo`/`routing.NewHandlers` in alongside identity/wallet; `NewRouter` gained a
  `routingHandlers` parameter (existing `health_test.go` updated to match).
- `backend/internal/routing/seed_data.go` — the canonical demo route/stop data, exported so
  both `cmd/seed` and the test suite share one source of truth instead of duplicating it:
  12 stops and 8 routes (4 forward Cape Town corridors + their 4 return trips — see the
  "return trips" decision below) across Cape Town corridors:
  - **Cape Town CBD – Khayelitsha** (5 legs, plus its return **Khayelitsha – Cape Town CBD**):
    Cape Town Station → Woodstock → Athlone → Mitchells Plain Town Centre → Khayelitsha Site C
    → Khayelitsha Town Centre.
  - **Athlone – Wynberg** (2 legs, plus its return **Wynberg – Athlone**): Athlone → Claremont
    → Wynberg.
  - **Cape Town CBD – Bellville** (2 legs, plus its return **Bellville – Cape Town CBD**):
    Cape Town Station → Parow → Bellville Station.
  - **Wynberg – Muizenberg** (2 legs, plus its return **Muizenberg – Wynberg**): Wynberg →
    Retreat → Muizenberg.
  - `RouteSeed`/`reverseRoute` build each return route from its forward counterpart: same
    stops, legs reversed, fares mirrored leg-for-leg.
  - `SeedCorridors(ctx, repo)` does the actual idempotent seeding (stops/routes matched by
    name — no DB uniqueness constraint on either, that name lookup is the idempotency check —
    and a route's legs are only inserted the first time that route has none) and returns an
    `error` instead of exiting, so it's callable from tests too.
- `backend/cmd/seed/main.go` — now just calls `routing.SeedCorridors` and prints the SEEDED
  DATA summary (all stops, all routes with ordered legs/fares, and which stops are
  interchanges). Interchanges are computed from `routing.ForwardCorridors` only (not every
  seeded route row), since a corridor and its own return trip share every stop by
  construction and would otherwise make every stop look like an "interchange": **Athlone**
  (CBD–Khayelitsha ⋂ Athlone–Wynberg), **Wynberg** (Athlone–Wynberg ⋂ Wynberg–Muizenberg), and
  **Cape Town Station** (CBD–Khayelitsha ⋂ CBD–Bellville).
- Tests:
  - `backend/internal/routing/graph_test.go` — pure unit tests against synthetic in-memory
    routes (no DB): direct path + fare sum, multi-hop via interchange, no-path (disconnected),
    direction matters (reverse of a route finds nothing), direct preferred over a
    cheaper-but-transferred alternative, same-stop origin/dest rejected.
  - `backend/internal/routing/integration_test.go` — against a real Postgres (skips if
    unreachable, matching Stage 0-2's pattern): seeds a small synthetic fixture (independent of
    `cmd/seed`'s data, uniquely named per run) and exercises `Search` through the real repo
    for direct, multi-hop, and no-path (reverse direction) cases. Since this runs against the
    shared dev DB rather than a disposable one, the fixture rows are deleted via `t.Cleanup`
    so they don't leak into `cmd/seed`'s output.
  - `backend/internal/routing/corridor_test.go` — against the real seeded demo corridors
    (`routing.SeedCorridors`, idempotent, not cleaned up afterward — same persistent data
    `cmd/seed` itself writes): confirms the original direct search is unaffected by adding
    return routes, confirms the new return-trip direction now succeeds with the mirrored
    fare, confirms Khayelitsha Town Centre ↔ Bellville Station is now genuinely connected
    (1 transfer via Cape Town Station — this pair used to be the stage's no-path example, see
    decision below), and confirms Khayelitsha Town Centre ↔ Muizenberg is still correctly
    unreachable within one transfer.

Decisions / deviations from the original plan:
- Chose **stop ids or exact stop names** for `from`/`to` (brief said "your call, keep it
  simple") — no fuzzy/partial name matching.
- Path search is implemented in Go over an in-memory load of all routes/legs rather than a
  recursive SQL query — simpler to read and test, and fine at this dataset size; would need
  revisiting if the route graph grows large.
- Limited multi-hop support to exactly one transfer, as explicitly permitted by the brief.
  The algorithm is a bounded search (all route pairs × shared stops) rather than a general
  Dijkstra/BFS, since one transfer is the entire supported scope for the MVP.
- `GET /routes*` endpoints are public (no `identity.RequireAuth`) since route/fare data isn't
  sensitive, unlike the wallet endpoints — a deviation from the "everything behind auth"
  pattern established in Stage 2, called out here since it's a deliberate choice.
- **Return-trip travel is seeded as separate directional route rows rather than making the
  graph bidirectional.** Real minibus taxi associations typically dispatch each direction as
  its own route from its own rank (often with its own numbering, and potentially its own
  fares), so a corridor and its return trip being two distinct route rows is the more
  faithful model, not a simplification — matches how associations actually file routes per
  direction and avoids added complexity/risk in the already-tested `Search` algorithm (which
  needed zero changes: extra route rows just widen the search space it already walks). Fares
  are mirrored leg-for-leg for now; a comment in `seed_data.go` flags that real per-direction
  fares (e.g. peak-direction pricing) could differ. One consequence worth calling out: adding
  the "Khayelitsha - Cape Town CBD" return route made Khayelitsha Town Centre ↔ Bellville
  Station — this stage's original no-path example — genuinely connected (1 transfer via the
  Cape Town Station interchange), since a real 1-transfer itinerary now exists. That's correct
  behavior, not a bug; Khayelitsha Town Centre ↔ Muizenberg replaced it as the no-path example
  (2 transfers apart even with return routes in place).

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .` (clean for routing code — the one
`gofmt -l` hit is pre-existing in `internal/identity/models.go`, unrelated to this stage), and
`go mod tidy` all pass with no `go.mod`/`go.sum` changes. Full test suite passes against a
live Postgres, including the routing unit, integration, and real-corridor tests. End-to-end
verified by hand: seeded, started the server, and curled a direct search (Cape Town Station →
Khayelitsha Town Centre, 5 legs, 3500 cents, unaffected by adding return routes), a multi-hop
search (Cape Town Station → Wynberg via the Athlone interchange, 1500 + 1100 = 2600 cents
across two segments/routes), the new return-trip direction (Khayelitsha Town Centre → Cape
Town Station, direct, mirrored fare 3500 cents), the newly-connected pair (Khayelitsha Town
Centre → Bellville Station, 1 transfer via Cape Town Station, 3500 + 1100 = 4600 cents), a
still-disconnected pair (Khayelitsha Town Centre → Muizenberg, clean 404), and `GET /routes`.

**Follow-up test-hygiene fix (2026-07-13):** flagged during this stage's work — the wallet
and identity integration tests (`backend/internal/wallet/ledger_test.go`,
`backend/internal/identity/integration_test.go`) generated phone numbers from either a plain
in-process counter (`seedCounter`, wallet) or a single hardcoded value (`+27821110000`,
identity), both of which restart/repeat from scratch on every process run. Running the suite
more than once against the same persistent Postgres (rather than a freshly reset one) made
these tests collide with rows a previous run had already created and fail with
"already exists" / 409s — only the routing tests (which already use a per-call
`time.Now().UnixNano()` suffix) survived repeat runs unscathed.

Fixed by generating identifiers the same way the routing tests do — unique per call, not
reset per process — rather than adding cleanup: `wallet.uniquePhone` now combines
`time.Now().UnixNano()` with an atomic counter (guards against two calls landing in the same
nanosecond), and identity's `TestRegisterLoginMe` generates its phone the same way. No
cleanup (`t.Cleanup`) was added on top of this: with truly unique identifiers the created rows
never collide with anything else again, so cleanup would only be about tidying up a dev-only
database, not correctness — and hand-written cascading deletes across
accounts/ledger_transactions/ledger_postings turned out to be genuinely risky (see below), so
skipping that trade was the safer call. This intentionally mirrors `routing/corridor_test.go`
already leaving its (idempotent, reusable) seed rows in place rather than every DB-backed test
in the repo cleaning up after itself.

No test assertions changed — only how test data is provisioned. Confirmed: `go build ./...`,
`go vet ./...`, `gofmt -l .` (same pre-existing `internal/identity/models.go` hit, unrelated),
and `go mod tidy` all clean, and the full suite (`go test ./...`) passes three times in a row
against the same live Postgres with no reset in between.

**Aside (not a bug in the fix, informational only):** while investigating this, an attempt to
hand-clean leftover test rows via direct SQL (deleting only the `ledger_postings` owned by
test-created accounts, not the full set of postings for a shared transaction) tripped the
Stage 2 zero-sum balance trigger, since a transaction's postings must be deleted as a complete
set for the deferred check to see a balanced (empty) result. Postgres's transactional
statement semantics rolled the failed statement back cleanly with no partial corruption, but
it's a good demonstration of why this fix avoids ad-hoc/partial ledger deletions in test
cleanup code.

Next: Stage 4 — telemetry

---

## Stage 4 — telemetry — DONE (2026-07-13)

Built:
- `backend/internal/telemetry/` — the telemetry module:
  - `store.go` — `VehicleStateStore`, a concurrency-safe in-memory
    `map[uuid.UUID]VehicleState` guarded by a single `sync.RWMutex`. Holds,
    per vehicle: `RouteID`, `DriverID`, `Lat`/`Lng`, `SeatsTotal`,
    `SeatsAvailable`, `Online`, `LastUpdated`. **In memory only, not
    Postgres** — positions reset on server restart (accepted MVP trade-off,
    avoids introducing Redis this stage) and no GPS history/track is
    persisted (that's Analytics' job later, per the stage brief). "Offline"
    is modeled as **absent from the map** rather than an `online=false` row,
    so a disconnected vehicle is automatically excluded from route
    snapshots with no separate cleanup pass. `GoOnline`/`GoOffline`/
    `UpdatePosition`/`AdjustSeats`/`SetSeatsAbsolute`/`Get`/`ListByRoute` all
    copy values in and out — callers never share mutable state with the
    store. Seat writes (`AdjustSeats`, `SetSeatsAbsolute`) always clamp to
    `[0, seats_total]`.
  - `hub.go` — `Hub`, a per-route pub/sub fan-out. `Subscribe(routeID)`
    hands back a `*Subscriber` with a buffered channel (32); `Publish`
    iterates that route's subscribers under an `RWMutex.RLock` and does a
    **non-blocking** `select`-with-`default` send to each — a slow/stuck
    commuter has updates dropped for it rather than blocking the publisher,
    which is always the driver ingestion path. This is the concurrency
    property the stage is actually testing: driver writes never wait on
    commuter reads.
  - `view.go` — `VehicleView`, the JSON-serializable projection of
    `VehicleState` sent over REST/WS (ids as strings, timestamp as RFC3339).
  - `handlers.go` — REST + WS endpoints (see below), plus `bearerToken`
    (Authorization header, falling back to a `token` query param — needed
    because browsers' WebSocket API can't set custom handshake headers, so
    a real commuter/driver web client has no choice but the query param;
    `cmd/wsdriver` demonstrates the header form since a Go client can use
    either).
- **WebSocket library: `github.com/gorilla/websocket`**, not `coder/websocket`
  — it's the library CLAUDE.md's stack already anticipated, is
  battle-tested, and its explicit `Upgrader`/`Conn.WriteJSON`/`ReadJSON` API
  maps directly onto the hub/fan-out pattern used here (one goroutine per
  connection doing explicit non-blocking-via-hub writes, no implicit
  background goroutines to reason about).
- Endpoints wired into `internal/server/router.go`:
  - `GET /ws/driver?route_id=<id>[&token=<jwt>]` — **not** behind
    `identity.RequireAuth` middleware, since the JWT must be validated
    *before* the HTTP→WS upgrade completes and middleware can't see inside
    that; `DriverWS` parses/validates the token itself via `bearerToken` +
    `tokens.Parse`. Requires role `driver`, an explicit `route_id` (going
    online only makes sense "online on a route" — no separate two-step
    "go online" call), a `drivers` row for the caller, and an **active**
    `vehicle_assignments` row for that driver (reusing Stage 1 data, not
    trusting a client-supplied vehicle id). On successful upgrade: marks
    the assigned vehicle online in the store (`seats_total` = the vehicle's
    Stage 1 `capacity`) and publishes an `update` event; on any
    disconnect (clean or not, via `defer`): marks it offline and publishes
    an `offline` event. Read loop accepts `{lat,lng}` position updates,
    `{seats_available}` (absolute) or `{seats_delta}` (relative) seat
    changes, or a bare `{heartbeat:true}` no-op — each valid update
    publishes to the hub.
  - `GET /ws/commuter?route_id=<id>` — **deliberately public, no auth** (see
    decision below). Subscribes to the hub for that route, sends an initial
    `{"type":"snapshot","vehicles":[...]}` of currently-online vehicles on
    that route, then streams `{"type":"update","vehicle":{...}}` /
    `{"type":"offline","vehicle_id":"..."}` events as they're published. A
    background goroutine drains incoming WS frames (gorilla requires an
    active reader to detect the peer closing) while the main goroutine
    selects between the hub channel and that close signal — one writer,
    one reader per connection, satisfying gorilla's concurrency contract.
  - `GET /telemetry/vehicles?route_id=<id>` — plain REST snapshot (no WS),
    for debugging and a map's initial load.
  - `POST /telemetry/seats` (driver only, behind `RequireAuth` +
    `RequireRole(driver)`) — `{delta}` or `{seats_available}`, an
    alternative to sending seat changes over the driver's own WS stream.
    Resolves the caller's active vehicle assignment the same way `DriverWS`
    does; 409 if that vehicle isn't currently online in the store (i.e. no
    live `/ws/driver` connection for it).
- `backend/internal/identity/repo.go` — added `GetVehicleByID` and
  `GetActiveVehicleAssignmentByDriverID` (both reused by telemetry;
  `GetActiveVehicleAssignmentByDriverID` relies on Stage 1's partial unique
  index guaranteeing at most one active assignment per driver).
- `backend/internal/server/router.go`, `backend/cmd/server/main.go` — wired
  `telemetry.NewVehicleStateStore`/`NewHub`/`NewHandlers` alongside the
  existing modules; `NewRouter` gained a `telemetryHandlers` parameter
  (`health_test.go` updated to match, same as every prior stage).
- `backend/cmd/wsdriver/main.go`, `backend/cmd/wscommuter/main.go` —
  standalone `go run`-able CLI clients for manual end-to-end verification
  without a browser (PowerShell can't easily drive raw WebSockets). See
  "Verified" below for exact commands.
- Tests:
  - `backend/internal/telemetry/store_test.go` — pure unit tests, no DB:
    `TestConcurrentUpdatesAndReads` (many goroutines doing position
    updates, seat deltas, route-snapshot reads, and online/offline churn
    against a shared store concurrently — asserts no data loss/corruption
    after `wg.Wait()`, run with `-race` to catch data races — see the
    known local-environment limitation below), `TestSeatClampingNeverExceedsBounds`
    (delta and absolute writes both clamp to `[0, seats_total]`),
    `TestGoOfflineRemovesFromRouteSnapshot`, `TestUpdatePositionOnUntrackedVehicleFails`.
  - `backend/internal/telemetry/integration_test.go` — against a real
    Postgres (skips if unreachable, same pattern as every prior stage) and
    real WebSocket connections over `httptest.NewServer`:
    `TestDriverUpdatePropagatesToCommuterOnSameRoute` — a commuter on the
    driver's route receives the initial empty snapshot, then an `update`
    event the instant the driver connects (vehicle online, correct
    `seats_total`), then a position update, then a seat-delta update,
    confirms the REST snapshot agrees while online, then an `offline` event
    the instant the driver's connection closes — while a commuter
    subscribed to a *different* route receives none of it, proving
    per-route isolation. `TestDriverWSRejectsWrongRole` — a commuter JWT is
    rejected with 403 on `/ws/driver`.

Decisions / deviations from the original plan:
- **`GET /ws/commuter` requires no auth.** Live position/seat-state isn't
  sensitive the way wallet/fare data is, and a commuter should be able to
  see the live map before logging in — this mirrors Stage 3's decision to
  leave `GET /routes*` public rather than the "everything behind auth"
  default. `GET /telemetry/vehicles` (its REST-snapshot counterpart) is
  public for the same reason.
- **JWT for `/ws/driver` is validated manually inside the handler, not via
  `identity.RequireAuth` middleware**, since the handshake needs to
  authenticate before `Upgrade()` runs and there's no clean way to run
  chi middleware "before upgrade, after auth" here. This also required
  supporting the token via a `token` query param (in addition to the
  `Authorization` header) since browsers' `WebSocket` constructor cannot
  set custom request headers — an unavoidable constraint of the WS
  handshake, not a shortcut.
- **"Going online" requires `route_id` up front on the WS URL**, not a
  separate prior "go online" REST call — the brief allowed either, and
  folding it into the WS connect keeps the state machine simpler (one
  fewer place where "online but no route" could exist).
- **Slow-consumer handling is "drop", not "coalesce."** `Hub.Publish` uses
  a non-blocking buffered-channel send (32-deep) and simply skips a
  subscriber whose mailbox is full, rather than replacing its oldest
  buffered message with the newest. Simpler to reason about and correct
  for an MVP demo's traffic levels; a coalescing hub (keep only the latest
  per-vehicle event) would be the natural upgrade if commuter fan-out ever
  needs to scale further.
Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
(added `github.com/gorilla/websocket` cleanly, no other diff) all pass. Full
test suite (`go test ./...`), including the telemetry unit and
WebSocket-over-`httptest` integration tests, passes against a live
Postgres. `go test -race ./...` also passes cleanly across every package
(including `internal/telemetry`) with **no data races detected**, once a C
toolchain (MSYS2's `ucrt64` gcc) was installed on this dev machine —
confirming `VehicleStateStore`'s single-`sync.RWMutex`/always-copy-in-out
design and `Hub`'s `RWMutex`-guarded subscriber map hold up under the race
detector, not just the plain test runner. No race-related findings, so no
code changes were needed as a result of running it. End-to-end verified by hand: started the server, ran `cmd/seed`,
logged in as a seeded driver, ran `cmd/wsdriver` against a seeded route to
go online and stream simulated positions, ran `cmd/wscommuter` against the
same route in a second terminal and watched it receive the snapshot →
online update → position updates live, confirmed a `cmd/wscommuter` on a
*different* route saw nothing, confirmed `GET /telemetry/vehicles` reflected
the same live state over plain REST, and confirmed killing `cmd/wsdriver`
(Ctrl+C) made the vehicle disappear from both the commuter WS stream
(`offline` event) and the REST snapshot.

Next: Stage 5 — boarding (QR scan)

---

## Stage 5 — boarding (fare-on-scan) — DONE (2026-07-14)

Built:
- `backend/internal/boarding/` — the boarding module. **Fuses, not
  reimplements**: identity/JWT (Stage 1) via `identity.RequireAuth`/
  `RequireRole` at the router, the wallet ledger's `ChargeFare` (Stage 2)
  called directly for the actual charge, routing's route/leg data (Stage 3)
  for pricing, and the telemetry `VehicleStateStore`/`Hub` (Stage 4) for the
  online/assignment check and seat decrement + broadcast. No new ledger
  path, no duplicated driver/vehicle lookups.
  - `models.go` — `PassPayload{CommuterID, RouteID, FromStopID, ToStopID,
    FareCents, IssuedAt, ExpiresAt, Nonce}`. `FareCents` is resolved once at
    issue time and carried *inside* the signed payload, so a scan never has
    to (and can't be tricked into) re-deriving or trusting a different fare.
    `Nonce` (a UUID) is the unique per-pass identifier.
  - `token.go` — `Signer`, a minimal self-contained HMAC-SHA256 scheme built
    directly on `crypto/hmac`/`crypto/sha256` rather than a JWT library: the
    token is `base64url(JSON payload) + "." + base64url(HMAC-SHA256 over the
    payload segment)`. Chosen over reusing `golang-jwt` because a boarding
    pass has a different trust boundary and lifecycle than an auth JWT (very
    short TTL, signed by a *different* secret than login tokens, and no
    claims/registered-claims baggage needed) — sharing the JWT library would
    have been convenient but would blur "this proves who you are" (Stage 1)
    with "this proves what you're allowed to board" (Stage 5). `Verify` uses
    `hmac.Equal` (constant-time) and returns a distinct error for a
    malformed token vs. a bad signature; it does **not** check expiry itself
    — that's a separate, distinctly-reported check in `ScanPass`, per the
    stage brief's "each failing cleanly with a distinct status/error."
  - `handlers.go` — `Handlers.IssuePass` (`POST /boarding/pass`, commuter
    only) and `Handlers.ScanPass` (`POST /boarding/scan`, driver only).
- `backend/internal/routing/graph.go` — added `FareForSegment(legs,
  fromStopID, toStopID) (fareCents int64, ok bool)`, a thin exported wrapper
  around the existing unexported `directSegment` helper `Search` already
  uses. This is the one new helper boarding needed from Stage 3: pricing a
  pass is "the direct fare on *this one* route between two stops," not a
  cross-route search, so it reuses `directSegment`'s same increasing-
  sequence rule rather than calling `Search` (which searches across all
  routes and would happily return a cheaper path on a *different* route than
  the one on the pass).
- `backend/internal/telemetry/view.go` — added `ToView(VehicleState)
  VehicleView`, the exported form of the already-existing unexported
  `toView`, so boarding can build the same WS broadcast payload as telemetry
  itself after decrementing seats, without duplicating the projection.
- **No changes to Stage 2's `/fare/charge` or `ChargeFare` signature** — the
  boarding brief allowed adjusting it if needed, but `ChargeFare(ctx,
  commuterUserID, vehicleID, fareCents, idempotencyKey, platformPct,
  driverPct)` already accepted everything boarding needed. `ScanPass` calls
  it with the pass's `Nonce` as `idempotencyKey`, and all of Stage 2's
  original tests keep passing unchanged.
- `POST /boarding/pass` (commuter, behind `RequireAuth` + `RequireRole`):
  `{route_id, from_stop_id, to_stop_id}` → looks up the route (404 if
  missing), loads its legs, prices via `routing.FareForSegment` (404 "no
  valid path" if the stops aren't in order on that route), builds and signs
  a `PassPayload` with `ExpiresAt = now + config.BoardingPassTTL`. Returns
  `{pass_token, expires_at, fare_cents}`.
- `POST /boarding/scan` (driver, behind `RequireAuth` + `RequireRole`):
  `{pass_token}` →, **in this exact order**:
  1. **Signature.** `Signer.Verify` (constant-time). Malformed or
     tampered → 401 `"invalid or tampered pass"`. No charge, no seat change.
  2. **Expiry.** `PassPayload.Expired(now)`. Expired → 410 `"pass has
     expired"`. No charge, no seat change.
  3. **Driver/vehicle/route match.** Resolves the caller's driver profile
     (Stage 1) and active vehicle assignment (Stage 1), then checks the
     telemetry store (Stage 4): the vehicle must be present (online) and its
     tracked `RouteID` must equal the pass's `RouteID`. Any mismatch
     (no driver profile, no active assignment, vehicle offline, or online on
     a *different* route) → 409 `"driver is not online on this pass's
     route"`. No charge, no seat change.
  4. **Charge.** `wallet.Repo.ChargeFare(ctx, payload.CommuterID,
     assignment.VehicleID, payload.FareCents, payload.Nonce, ...)` — the
     pass's nonce *is* the ledger idempotency key. 402 on insufficient
     funds (no seat change), 422 if the vehicle unexpectedly has no active
     driver row (defensive — should already be excluded by step 3).
  5. **Seat decrement, tied to freshness not to the scan itself.** Only when
     `ChargeFare` returns `replayed=false` does `ScanPass` call
     `telemetry.AdjustSeats(vehicleID, -1)` and publish the update over the
     hub. A replayed scan reports the *current* `seats_remaining` from the
     store without touching it — this is what makes a double-scan of the
     same pass debit the wallet exactly once **and** decrement seats exactly
     once, using the *same* freshness signal for both, rather than two
     independently-idempotent-but-possibly-inconsistent checks.
  Returns `{transaction_id, fare_cents, platform_cents, driver_cents,
  owner_cents, seats_remaining, replayed}` — 201 on a fresh charge, 200 on a
  replay.
- `backend/internal/config` — added `BoardingHMACSecret` (env
  `BOARDING_HMAC_SECRET`, dev-only default, documented in `.env.example` —
  deliberately a **different** secret from `JWT_SECRET`, since a leaked
  boarding secret and a leaked auth secret are different blast radii) and
  `BoardingPassTTL` (env `BOARDING_PASS_TTL_SECONDS`, default **180s**, i.e.
  3 minutes — within the brief's suggested 2-5 min window).
- `backend/internal/server/router.go`, `backend/cmd/server/main.go` — wired
  `boarding.NewHandlers` alongside the existing modules; `NewRouter` gained a
  `boardingHandlers` parameter (`health_test.go` updated to match, same
  pattern as every prior stage).
- Tests:
  - `backend/internal/boarding/token_test.go` — pure unit tests, no DB:
    sign→verify round-trips the payload; verify rejects a token signed with
    a different secret; verify rejects a tampered payload byte; verify
    rejects a tampered signature byte (flipped in the *middle* of the
    signature segment, not the last character — a base64url encoding's
    final character can carry unused padding bits that don't change the
    decoded byte value, which would make a last-byte flip a false-negative
    tamper test); verify rejects assorted malformed token shapes;
    `PassPayload.Expired` at/before/after the boundary.
  - `backend/internal/boarding/boarding_test.go` — against a real Postgres
    (skips if unreachable, same pattern as every prior stage) and the real
    HTTP handlers (via `chi.Router` + `identity.RequireAuth`, driven with
    real bearer tokens — "test it as a raw token over HTTP" per the brief).
    `seedFixture` creates a route+leg, an owner/vehicle/driver with an
    active assignment, and a commuter, then marks the vehicle online on the
    route directly in the `VehicleStateStore` (equivalent to what a real
    `/ws/driver` connection does, without needing a live WS connection in
    every test):
    - `TestHappyPath_IssueScanChargeSeatDecrement` — issue → scan: ledger
      charged (split sums to the fare), commuter balance debited by exactly
      the fare, seats decremented by exactly 1, receipt fields correct.
    - `TestTamperedPass_Rejected` — flipped signature byte → 401, no charge,
      no seat change.
    - `TestExpiredPass_Rejected` — a pass issued through a handler wired
      with a 1-nanosecond TTL (deterministic, no real-time sleep-then-race)
      → 410 once past that TTL, no charge, no seat change.
    - `TestDoubleScan_IdempotentReplay` — same pass scanned twice → same
      `transaction_id`, second response `replayed:true`, wallet debited
      exactly once, seats decremented exactly once (verified both via the
      store directly and via the replay's own reported `seats_remaining`).
    - `TestInsufficientFundsRejected` — commuter balance below the fare →
      402, wallet unchanged, no seat change.
    - `TestWrongDriverRoute_Rejected` — a second, *online* driver on a
      *different* route scans the first pass → 409, no charge, no seat
      change.
    - `TestDriverOffline_Rejected` — the assigned driver's vehicle is taken
      offline in the store (as a closed `/ws/driver` connection would do)
      before scanning → 409.

Decisions / deviations from the original plan:
- **A boarding pass prices against one specific route** (`FareForSegment`
  over that route's legs), not a cross-route `routing.Search`. The pass
  payload already carries a `route_id` the commuter chose (e.g. from a
  `/routes/search` result they're about to board), so re-running a general
  search at issue time could silently substitute a cheaper *different*
  route than the one the commuter is standing at — `FareForSegment` prices
  exactly the ride the pass claims to be for.
- **Own HMAC scheme instead of reusing `golang-jwt`** (already used for auth
  in Stage 1). A hand-rolled `base64url(payload).base64url(hmac)` format was
  simpler to reason about for a short-lived, single-purpose, QR-sized token,
  keeps the boarding trust boundary (a different secret, a much shorter
  TTL) visibly separate from the auth trust boundary in the code, and avoids
  JWT registered-claims fields that don't apply here. `hmac.Equal` still
  gives the constant-time comparison the brief explicitly asks for.
- **Expiry is checked separately from signature verification**, not folded
  into `Verify`, so a scan can distinguish "this pass was forged/corrupted"
  (401) from "this pass was real but is too old" (410) — both were called
  out as needing distinct statuses in the brief.
- **The seat decrement's freshness check reuses `ChargeFare`'s own
  `replayed` return value** rather than a second, independent idempotency
  mechanism (e.g. tracking scanned nonces in telemetry). This guarantees the
  wallet debit and the seat decrement can never disagree about whether a
  given scan was "the first" one — they're both gated on the same boolean
  from the same ledger call.
- **The driver/route match check reuses the live telemetry store, not just
  the Stage 1 `vehicle_assignments` row.** A driver can be correctly
  assigned to a vehicle in Stage 1's data yet not actually be online (no
  live `/ws/driver` connection) or online on a *different* route than the
  one on the pass — `ScanPass` requires all three (assigned, online, online
  on *this* route) to match, which is what `TestDriverOffline_Rejected` and
  `TestWrongDriverRoute_Rejected` cover.
- Kept `BOARDING_HMAC_SECRET` **distinct** from `JWT_SECRET` even though
  both are dev-only defaults today — a real deployment rotating one
  shouldn't have to reason about the other, and a leak of one has a
  different blast radius (forge boarding passes vs. forge full auth
  sessions) than the other.

SCOPE HONESTY (per CLAUDE.md and the stage brief): this stage produces and
verifies the signed pass token a QR code would carry — there is no QR
rendering/scanning yet (a later frontend stage) — and proves the
cryptographic + financial flow only. It does not add proximity/geofencing;
boarding still assumes the commuter physically handed their phone to (or was
scanned by) the driver.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` changes — boarding needed no new
dependencies). Full test suite (`go test ./...`), including the new
boarding unit + integration tests, passes against a live Postgres, and `go
test -race ./...` passes cleanly across every package (MSYS2 `ucrt64` gcc
toolchain, same as Stage 4) with **no data races detected** and no
regressions in Stage 2's original wallet/ledger tests. End-to-end verified
by hand: seeded, started the server, logged in as a seeded commuter and
driver, ran `cmd/wsdriver` to bring driver 1's vehicle online on "Cape Town
CBD - Bellville", `POST /boarding/pass` for the full Cape Town Station →
Bellville Station ride (fare 1100 = 700 + 400), `POST /boarding/scan` →
fresh charge (`replayed:false`, split 110/275/715, `seats_remaining: 15`,
seats 16→15 confirmed via `GET /telemetry/vehicles`), then scanned the
*same* pass again → identical `transaction_id`, `replayed:true`,
`seats_remaining` still 15 (no double charge, no double decrement).

Next: Stage 6 — request-a-stop

---

## Stage 6 — request-a-stop — DONE (2026-07-14)

Built:
- `backend/internal/telemetry/alerts.go` — `DriverAlertHub`, a per-driver
  pub/sub mailbox registry, deliberately parallel to `Hub` (Stage 4) but
  keyed by **driver id** instead of route id: `Subscribe(driverID)` hands
  back a `*DriverAlertSub` with a buffered channel (8), and `Send(driverID,
  msg)` does the same **non-blocking**, drop-on-full-mailbox send as
  `Hub.Publish` — a stop-request publish must never block on a slow/stuck
  driver connection. `Send` returns whether at least one mailbox actually
  received the message, so a caller can tell "driver was truly reachable"
  from "telemetry said online but the WS had just dropped."
- `backend/internal/telemetry/handlers.go` — **`/ws/driver` is now
  bidirectional.** Previously the connection only ever read from the driver
  (position/seat updates) and never wrote anything back. It now also
  subscribes to a `DriverAlertHub` mailbox for the caller's driver id and
  pushes any alert queued there straight to the connection. Since
  gorilla/websocket allows exactly one concurrent reader and one concurrent
  writer per connection, the read loop moved onto its own goroutine
  (forwarding decoded `driverMessage`s over an internal channel) while the
  original goroutine becomes the sole writer, `select`-ing between incoming
  driver messages, pushed alerts, and the read goroutine's exit signal —
  the same single-writer discipline `CommuterWS` already used for its
  hub-fan-out select loop. `NewHandlers` gained a `*DriverAlertHub`
  parameter (all three call sites — `cmd/server/main.go`,
  `internal/server/health_test.go`, `internal/telemetry/integration_test.go`
  — updated to match).
- `backend/internal/stops/` — the new stops module, built entirely on top
  of existing infrastructure (no new persistence layer, no duplicated
  lookups):
  - `models.go` — `Request{ID, CommuterID, RouteID, StopID, StopName,
    RequestedAt, Status, MatchedDriverID, AckedAt}`. `Status` is one of
    `pending` (matched + alert delivered, awaiting ack), `unmatched` (no
    qualifying driver was reachable), `acknowledged`.
  - `store.go` — `Store`, an in-memory `map[uuid.UUID]Request` guarded by a
    single `RWMutex`, same shape as `telemetry.VehicleStateStore`.
    **In-memory only — resets on server restart**, same accepted MVP
    trade-off already made for Stage 4's live vehicle state (see CLAUDE.md
    SCOPE HONESTY).
  - `geo.go` — `haversineMeters`, the one distance primitive the matching
    rule needs.
  - `match.go` — the pure, DB-free matching algorithm (`FindApproachingDriver`),
    unit-tested with synthetic data (no DB, no WS):
    - **Approaching-driver rule** (documented in code, deliberately simple,
      per the stage brief's "approximate position-to-sequence mapping is
      fine — note the approximation"): a route's stops are given a 0-based
      sequence index (`StopSequenceIndex`) from its ordered legs (Stage 3).
      A driver's current route-progress is approximated as
      **`nearestStopIndex`** — the index of the geographically nearest stop
      (straight-line/haversine distance) to their last reported lat/lng.
      This is **not** true map-matching or geofencing: a driver just past a
      stop but still physically closer to it than to the next one reads as
      "at" that stop, not "just past" it. A driver qualifies as
      "approaching" a requested stop if `nearestStopIndex <=
      targetStopIndex` — i.e. they have not (as far as this approximation
      can tell) already passed it.
    - **Selection rule**: among qualifying (approaching, online, same-route)
      drivers, the one physically nearest (haversine distance from their
      live position to the requested stop's own lat/lng) is chosen. **Only
      one driver is alerted per request** in this MVP — simplest to reason
      about for a first cut; alerting every qualifying driver would be the
      natural extension if a single alerted driver turns out to be an
      unreliable pickup in practice. Both decisions are the brief's "your
      call" clauses, exercised and documented rather than left implicit.
  - `handlers.go` — `Handlers.RequestStop` (`POST /stops/request`, commuter
    only) and `Handlers.AckRequest` (`POST /stops/request/{id}/ack`, driver
    only). `RequestStop`: resolves the route (404 if missing), builds the
    route's ordered stop sequence from its legs (Stage 3,
    `routing.Repo.ListLegsForRoute` + per-stop `GetStopByID` — the same
    "small dataset, just loop" reasoning `routing.AllRoutesWithLegs` already
    uses), 404s if the requested stop isn't on that route, pulls every
    currently-online driver on the route from `telemetry.VehicleStateStore`
    (Stage 4), runs `FindApproachingDriver`, and — if a driver qualifies —
    pushes a `telemetry.AlertMessage{Type: "stop_request", RequestID,
    RouteID, StopID, StopName, RequestedAt}` through `DriverAlertHub.Send`.
    If no driver qualifies (or the matched driver's mailbox wasn't actually
    reachable — see `DriverAlertHub.Send`'s return value), responds 200 with
    `{status: "unmatched", driver_available: false}` — **a clean result, not
    an error**, per the stage brief. A successful match responds 201 with
    `{request_id, status: "pending", driver_available: true}`. `AckRequest`
    resolves the caller's driver profile (Stage 1), 403s if they aren't the
    request's `MatchedDriverID`, then calls `Store.Acknowledge` (idempotent —
    acking an already-acknowledged request is a no-op, not an error, since a
    driver double-tapping "picked up" shouldn't see a failure).
  - **Commuter notification on ack is not implemented** — the stage brief
    called this out as optional ("optionally notifies the commuter"). The
    commuter's only live connection is the route-wide `/ws/commuter` stream
    (vehicle telemetry, not per-request), and adding a commuter-specific
    push channel for this one field felt like scope creep for an MVP demo;
    a commuter can poll `GET /stops/request/{id}` in a later stage if this
    needs surfacing. Flagged here rather than silently dropped.
- `backend/internal/server/router.go` — wired `stops.Handlers` in:
  `POST /stops/request` under the existing commuter `RequireRole` group,
  `POST /stops/request/{id}/ack` under the existing driver `RequireRole`
  group. `NewRouter` gained a `stopsHandlers` parameter (`health_test.go`
  updated to match, same pattern as every prior stage).
- `backend/cmd/server/main.go` — constructs `telemetry.NewDriverAlertHub()`
  once (shared between `telemetry.NewHandlers` and `stops.NewHandlers`),
  and `stops.NewStore()` / `stops.NewHandlers(...)`.
- `backend/cmd/wsdriver/main.go` — since `/ws/driver` is now bidirectional,
  the client gained its own concurrent read loop that prints any
  server-pushed message (currently just stop-request alerts) as it arrives,
  alongside its existing position-update write loop — demonstrates the new
  push channel without needing a browser.
- Tests:
  - `backend/internal/stops/match_test.go` — pure unit tests, no DB, no WS:
    `StopSequenceIndex` on/off-route; `FindApproachingDriver` matches an
    approaching driver, rejects a driver whose nearest stop is past the
    requested one, picks the nearer of two qualifying drivers, returns no
    match when every driver has passed the stop (or none are online), and
    returns no match when the requested stop isn't on the route at all.
  - `backend/internal/stops/integration_test.go` — against a real Postgres
    (skips if unreachable, same pattern as every prior stage) and a real
    `/ws/driver` WebSocket connection (so alert delivery is proven through
    the actual `DriverAlertHub`, not mocked): seeds a straight 3-stop,
    2-leg route (Origin → Mid → Dest) plus a driver/vehicle/commuter,
    drives `telemetry.GoOnline`/`UpdatePosition` directly (equivalent to
    what a real `/ws/driver` connect + position update would do) alongside
    an actual driver WS dial so the alert has somewhere real to land:
    - `TestApproachingDriverReceivesAlert` — a driver near Origin requesting
      pickup at Dest receives the `stop_request` alert on their own
      connection, with matching `request_id`/`stop_id`.
    - `TestDriverPastStopNotAlerted` — a driver whose nearest stop is Dest
      does not get alerted for a Mid-stop request (clean `unmatched`
      result).
    - `TestDriverOnDifferentRouteNotAlerted` — a driver online on a
      different route entirely is never considered.
    - `TestNoDriverOnline_CleanUnmatchedResult` — no driver online at all on
      the route → 200 `{status: "unmatched", driver_available: false}`, not
      an error.
    - `TestAckFlow_MarksRequestAcknowledged` — the matched driver acking the
      request gets back `{status: "acknowledged"}`.

Decisions / deviations from the original plan:
- **Position-to-route-progress is "nearest stop by straight-line distance,"**
  not true map-matching/geofencing — explicitly called out in `match.go`'s
  doc comment and above, per the stage brief's required scope honesty. This
  is the one approximation the whole feature rests on; everything else
  (sequence indexing, driver selection) is exact given that approximation.
- **Only the single nearest qualifying driver is alerted**, not every
  qualifying driver — the brief left this as an open call. Chose the
  simplest behavior for a first cut; broadening to multiple recipients
  would only need a loop change in `RequestStop`, not a `match.go` rewrite.
- **`/ws/driver`'s read loop moved onto its own goroutine** rather than
  bolting a second connection or a separate long-poll endpoint onto the
  driver client — this was the direct consequence of the brief's
  requirement that the *existing* driver connection be able to receive
  server-pushed alerts, and keeps the "one WS connection per driver" model
  intact instead of adding a second channel.
- **Commuter ack-notification was intentionally left out** (see above) —
  the brief explicitly allowed this ("optionally"), and there's no existing
  per-commuter live channel to hang it off of without adding new scope.
- **Active stop-requests are in-memory (`stops.Store`) and reset on
  restart** — explicitly called out in the brief as acceptable, matching
  Stage 4's `VehicleStateStore` precedent exactly.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` changes — stops needed no new dependencies).
Full test suite (`go test ./...`), including the new stops unit + WS
integration tests, passes against a live Postgres, and `go test -race ./...`
(MSYS2 `ucrt64` gcc toolchain, same as Stage 4/5) passes cleanly across
every package with **no data races detected** — including the new
`DriverAlertHub` under concurrent `Send`/`Subscribe`/`Unsubscribe` exercised
by the WS integration tests running alongside each other. End-to-end
verified by hand: seeded, started the server, brought a driver online near
a route's origin stop via `cmd/wsdriver`, fired `POST /stops/request` for a
downstream stop as a commuter and watched the alert print in the driver
client's terminal in real time; confirmed a driver already past the
requested stop, and a driver online on a different route, both correctly
receive nothing and the request reports `driver_available: false`. See the
PowerShell walkthrough below.

Next: Stage 7 — fuel disbursement (mock)

---

## Stage 7 — fuel disbursement (mock) — DONE (2026-07-14)

**SCOPE HONESTY (highest bar of any stage, per CLAUDE.md and the stage
brief):** there is no real eFuel / FuelOmat / VIU device anywhere in this
codebase. `backend/internal/fuel/viu_mock.go` carries a file-level doc
comment saying so explicitly, and every mock type/endpoint is named and
commented as a simulation of the hardware boundary. The **withholding**
half of this stage (`Allocate`) is the opposite: it is REAL double-entry
ledger accounting, reusing Stage 2's `accounts`/`ledger_transactions`/
`ledger_postings` tables and zero-sum trigger unchanged.

Built:
- `backend/migrations/000004_fuel_schema.{up,down}.sql`:
  - `ALTER TYPE account_type ADD VALUE 'fuel_account'` and `ALTER TYPE
    transaction_kind ADD VALUE 'fuel_allocation'` — additive-only, Stage 2's
    schema/trigger need no changes. (Down migration cannot drop a single
    enum value in Postgres without recreating the type; documented as an
    accepted MVP limitation rather than attempted.)
  - `vehicle_fuel_quotas` (`vehicle_id` PK, `owner_user_id`, `quota_cents`,
    `reserved_cents`, `used_cents`) — a **plain table, not a second ledger
    account per vehicle** (the brief explicitly allows either). Available-
    to-authorize is always `quota_cents - reserved_cents - used_cents`,
    enforced by a `CHECK (reserved_cents + used_cents <= quota_cents)`
    constraint, not just application code.
  - `fuel_authorizations` (`id`, `vehicle_id`, `litres`, `amount_cents`,
    `status` enum `reserved`/`confirmed`, `confirmed_at`) — the MOCK VIU's
    authorize-then-confirm session records; `id` is the `auth_reference` a
    real device would carry from authorize through to confirm.
- `backend/internal/wallet/`:
  - Added `AccountFuelAccount` ("fuel_account") and `KindFuelAllocation`
    ("fuel_allocation") to the existing `AccountType`/`TransactionKind`
    enums (`models.go`) — Stage 2's "add a fuel account type (or reuse the
    account model)" instruction taken literally.
  - **`Repo.InternalTransfer(ctx, ownerUserID, fromType, toType,
    amountCents, kind, metadata)`** — new generic primitive, factored out of
    `ChargeFare`'s lock/read/post pattern: moves money between two accounts
    owned by the same user as one balanced ledger transaction. This is what
    `/fuel/allocate` is built on (`owner_revenue -> fuel_account`), reused
    rather than duplicating the lock-then-balance-check logic a third time.
- `backend/internal/fuel/` — the new fuel module:
  - `models.go` — `VehicleQuota` (with `AvailableCents()`), `Authorization`,
    `AuthorizationStatus`. Package doc comment states the real-vs-mock split
    up front.
  - `repo.go` — the REAL-ledger half: `Allocate` (computes `withholdPct%` of
    the owner's current `owner_revenue` balance and calls
    `wallet.Repo.InternalTransfer`; errors `ErrNothingToAllocate` if revenue
    is zero rather than posting a no-op zero-amount transaction),
    `Balance` (fuel_account ledger balance, same `SUM(postings)` derivation
    as every other account), `FundVehicleQuota`, `VehicleQuotaFor`. A
    doc-comment block states the **anti-bypass property** structurally:
    fuel_account/vehicle-quota money only ever flows
    `owner_revenue -> fuel_account -> a vehicle's quota -> a MOCK VIU
    authorization` — there is no function anywhere in the package that
    posts value from fuel_account/vehicle_fuel_quotas toward
    commuter_wallet, driver_earnings, owner_revenue, or funding_source.
  - `viu_mock.go` — the MOCK VIU half, file-level comment says explicitly
    there is no real device on the other end: `AuthorizePump` (checks the
    vehicle's available quota under `SELECT ... FOR UPDATE`; if sufficient,
    inserts a `fuel_authorizations` row with `status='reserved'` and
    increments `reserved_cents` — a **reservation, not yet a final debit**;
    if insufficient, denies with a reason and the real available amount,
    reserving nothing) and `ConfirmPump` (moves `reserved_cents` to
    `used_cents` and marks the row `confirmed` — the actual settlement; a
    second confirm of the same `auth_reference` is a no-op, returning
    `already_confirmed: true` instead of debiting twice). A `TODO` comment
    on `ConfirmPump` flags that an unconfirmed reservation never expires in
    this MVP — a real system would need a timeout sweep to release it back
    to available quota, not implemented here.
  - `handlers.go` — HTTP surface: `Allocate`, `Balance`, `FundVehicleQuota`,
    `VehicleQuota` (all owner-only), `AuthorizePump`/`ConfirmPump` (the MOCK
    VIU endpoints — see routing decision below). `AuthorizePump` accepts
    **either** `litres` (converted via `pricePerLitreCents`) **or**
    `amount_cents` directly, documented as "keep it simple, work in cents
    underneath."
- `backend/internal/config`:
  - `FuelWithholdPct` (env `FUEL_WITHHOLD_PCT`, default **30**).
  - `FuelPricePerLitreCents` (env `FUEL_PRICE_PER_LITRE_CENTS`, default
    **2200**, i.e. R22.00/litre — a plausible dev-only default, not a live
    price feed).
- `backend/internal/server/router.go`, `backend/cmd/server/main.go` — wired
  `fuel.NewRepo`/`fuel.NewHandlers` in; `NewRouter` gained a `fuelHandlers`
  parameter (`health_test.go` updated to match, same pattern as every prior
  stage). Routes:
  - `POST /fuel/allocate`, `GET /fuel/balance`, `POST /fuel/vehicle/quota`,
    `GET /fuel/vehicle/quota?vehicle_id=` — all behind
    `identity.RequireAuth` + `identity.RequireRole(RoleOwner)`, in a new
    owner-only route group (the first one this router has needed — prior
    stages' owner endpoints were all `GET /wallet/balance`, which is
    role-generic).
  - `POST /fuel/viu/authorize`, `POST /fuel/viu/confirm` — **deliberately
    public, no auth** (see decision below).

Decisions / deviations from the original plan:
- **Per-vehicle quota is a plain table (`vehicle_fuel_quotas`), not a
  second ledger account per vehicle.** The brief explicitly allowed either.
  Funding a vehicle's quota from the owner's `fuel_account` does **not**
  post a new ledger transaction — the real money movement already happened
  in `Allocate` (`owner_revenue -> fuel_account`); earmarking a slice of
  that already-withheld balance to one vehicle is bookkeeping over money
  that's already left owner_revenue, not a second cross-account transfer.
  It's still checked against `fuel_account`'s live ledger balance minus
  everything already earmarked to other vehicles, so a vehicle's quota can
  never exceed what the owner actually withheld. Verified directly by
  `TestAntiBypass_FuelFundsNeverReachWalletOrPayout`, which asserts
  `FundVehicleQuota`/`AuthorizePump`/`ConfirmPump` add **zero** new
  `ledger_postings` rows — only `Allocate` ever does.
- **Authorize reserves, confirm settles** — modeled exactly as two separate
  states (`reserved_cents` vs `used_cents`) rather than debiting on
  authorize, mirroring how a real fuel-dispensing device actually works
  (hold the funds when the nozzle handshakes, settle once fuel actually
  flows). A second `/fuel/viu/confirm` on the same `auth_reference` is
  idempotent (not an error) — it reports `already_confirmed: true` and
  changes nothing, the same "replay-safe" pattern Stage 5's boarding scan
  and Stage 2's `ChargeFare` already use for their own idempotency keys.
- **Unconfirmed reservations are never released** in this MVP — flagged as
  a `TODO` in `viu_mock.go` rather than implemented. A real system would
  need a background sweep or authorize-TTL to return a stale hold's
  `reserved_cents` to available quota; out of scope here per the brief's
  explicit allowance ("a TODO comment is fine for MVP").
- **`/fuel/viu/authorize` and `/fuel/viu/confirm` require no auth.** Every
  other endpoint in this stage is owner-only, but these two stand in for a
  physical device's half of the conversation — a real VIU would
  authenticate with device-level credentials (a provisioned cert/API key),
  not a commuter/driver/owner JWT, and modeling that is out of scope for an
  MVP hardware simulation. Called out explicitly in `router.go` rather than
  left as an implicit gap.
- **A denied authorization is `200 {authorized:false, reason, max_amount}`,
  not an HTTP error status.** A real VIU integration needs to distinguish
  "the device/request itself was malformed" (4xx) from "the request was
  valid but the answer is no" (a clean decline) — the same reasoning
  Stage 6's `stops.RequestStop` already applied to `driver_available:
  false`.
- **`AuthorizePump` accepts litres OR amount_cents**, converting via the
  configured `FuelPricePerLitreCents`, rather than requiring one specific
  unit — kept simple per the brief ("just work in cents — keep it simple,
  document units"), documented in the handler's doc comment.
- `cmd/seed` was **not** modified for this stage — the PowerShell
  walkthrough below demonstrates the full flow by hand against the existing
  seeded owner/vehicle/driver/commuter, charging a fresh fare to fund
  `owner_revenue` rather than needing a new seed step.

Tests (`backend/internal/fuel/fuel_test.go`, against a real Postgres, skips
like every prior stage's integration tests if none is reachable):
- `TestAllocate_MovesExactWithholdPercentage` — `/fuel/allocate` withholds
  exactly `withholdPct%` of `owner_revenue` into `fuel_account`;
  `owner_revenue` drops by exactly that amount; the transaction's postings
  sum to zero.
- `TestAllocate_NothingToAllocateWhenRevenueZero` — zero revenue ->
  `ErrNothingToAllocate`, no transaction created.
- `TestFundVehicleQuota_CannotExceedFuelAccountBalance` — funding more than
  `fuel_account` holds is rejected (`wallet.ErrInsufficientFunds`); funding
  within it succeeds and is reflected in `quota_cents`/`AvailableCents()`.
- `TestFundVehicleQuota_RejectsVehicleNotOwnedByCaller` — an owner cannot
  fund a quota for another owner's vehicle.
- `TestAuthorizePump_WithinQuota_ReservesAndAuthorizes` — authorize within
  quota -> `authorized:true`, `reserved_cents` increases by the requested
  amount, available quota drops accordingly.
- `TestAuthorizePump_BeyondQuota_DeniedAndQuotaUnchanged` — authorize beyond
  quota -> `authorized:false` with the real `max_amount_cents`, and the
  quota row is completely unchanged (no partial reservation).
- `TestAuthorizePump_NoQuotaAllocated_Denied` — a vehicle with no quota ever
  funded is cleanly denied, not a 500.
- `TestConfirmPump_SettlesReservationCorrectly` — confirm moves
  `reserved_cents` to `used_cents` for exactly the reserved amount.
- `TestConfirmPump_SecondConfirmIsIdempotent_NoDoubleDebit` — the explicit
  brief requirement: a second confirm on the same `auth_reference` reports
  `already_confirmed:true` and `used_cents` does not move a second time.
- `TestConfirmPump_UnknownReference_NotFound` — confirming a nonexistent
  reference returns `ErrNotFound`, not a silent success.
- `TestAntiBypass_FuelFundsNeverReachWalletOrPayout` — runs the full
  allocate -> fund quota -> authorize -> confirm flow and asserts: (a)
  `FundVehicleQuota`/`AuthorizePump`/`ConfirmPump` create **zero** new
  `ledger_postings` rows (only `Allocate` does — confirmed by comparing
  `ledger_postings` row counts before/after each step), and (b) the total
  balance across every `commuter_wallet`, `driver_earnings`, and the
  `funding_source` account is completely unchanged by any fuel operation —
  the structural proof that fuel value cannot be cashed back out through
  the ledger.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` changes — fuel needed no new dependencies).
Full test suite (`go test ./...`), including the new fuel unit/integration
tests, passes against a live Postgres, and `go test -race ./...` (MSYS2
`ucrt64` gcc toolchain, same as Stages 4-6) passes cleanly across every
package with **no data races detected** and no regressions in any prior
stage's tests. End-to-end verified by hand against the seeded dev data —
see the PowerShell walkthrough below (seed -> charge a fare so
`owner_revenue` is non-zero -> owner `/fuel/allocate` -> `/fuel/balance` ->
fund a vehicle's quota -> MOCK VIU authorize within quota -> authorize
beyond quota, cleanly denied -> confirm -> replayed confirm, idempotent).

### PowerShell walkthrough

Assumes the Docker Postgres is running and `cmd/server`/`cmd/seed` use the
default `localhost:5432` (stop the native Windows Postgres service first if
it's shadowing that port — see CLAUDE.md Stage 0 note). Run from
`backend/`.

```powershell
# 1. Seed dev data (idempotent) and start the server in another terminal.
go run ./cmd/seed
go run ./cmd/server
```

```powershell
# 2. In a second terminal: log in as the seeded owner and driver 1, and
#    charge a couple of fares so owner_revenue is non-zero. Substitute the
#    vehicle/commuter ids cmd/seed printed for CA123456 / commuter 1 if
#    they differ from a prior run.

$ownerLogin = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000001","password":"Owner123!"}'
$ownerToken = $ownerLogin.token

$driverLogin = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000002","password":"Driver123!"}'
$driverToken = $driverLogin.token

$commuterLogin = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000004","password":"Commuter123!"}'
$commuterId = $commuterLogin.user_id   # or read from cmd/seed output

# vehicleId = CA123456's id, printed by cmd/seed
$vehicleId = "<paste CA123456's id from cmd/seed output>"

Invoke-RestMethod -Method Post http://localhost:8080/fare/charge `
  -Headers @{Authorization = "Bearer $driverToken"} -ContentType "application/json" `
  -Body (@{commuter_id=$commuterId; vehicle_id=$vehicleId; fare_cents=3500; idempotency_key=[guid]::NewGuid().ToString()} | ConvertTo-Json)

Invoke-RestMethod -Method Get http://localhost:8080/wallet/balance `
  -Headers @{Authorization = "Bearer $ownerToken"}
# -> owner_revenue balance should now be non-zero (65% of 3500 = 2275)
```

```powershell
# 3. Owner withholds 30% of owner_revenue into fuel_account.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/allocate `
  -Headers @{Authorization = "Bearer $ownerToken"}
# -> {"transaction_id": "...", "allocated_cents": 682, "withhold_pct": 30}

Invoke-RestMethod -Method Get http://localhost:8080/fuel/balance `
  -Headers @{Authorization = "Bearer $ownerToken"}
# -> {"balance_cents": 682}
```

```powershell
# 4. Owner funds vehicle CA123456's fuel quota from fuel_account.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/vehicle/quota `
  -Headers @{Authorization = "Bearer $ownerToken"} -ContentType "application/json" `
  -Body (@{vehicle_id=$vehicleId; amount_cents=500} | ConvertTo-Json)
# -> {"vehicle_id": "...", "quota_cents": 500, "reserved_cents": 0, "used_cents": 0, "available_cents": 500}
```

```powershell
# 5. MOCK VIU authorizes a pump session WITHIN quota.
$auth = Invoke-RestMethod -Method Post http://localhost:8080/fuel/viu/authorize `
  -ContentType "application/json" -Body (@{vehicle_id=$vehicleId; amount_cents=300} | ConvertTo-Json)
$auth
# -> {"authorized": true, "auth_reference": "...", "max_amount_cents": 300}

# 6. MOCK VIU authorizes a pump session BEYOND remaining quota (500-300=200 left) — DENIED.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/viu/authorize `
  -ContentType "application/json" -Body (@{vehicle_id=$vehicleId; amount_cents=250} | ConvertTo-Json)
# -> {"authorized": false, "reason": "requested amount exceeds available fuel quota", "max_amount_cents": 200}
```

```powershell
# 7. MOCK VIU confirms the first (authorized) pump session.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/viu/confirm `
  -ContentType "application/json" -Body (@{auth_reference=$auth.auth_reference} | ConvertTo-Json)
# -> {"vehicle_id": "...", "amount_cents": 300, "already_confirmed": false}

# Replaying the same confirm is idempotent — no double debit.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/viu/confirm `
  -ContentType "application/json" -Body (@{auth_reference=$auth.auth_reference} | ConvertTo-Json)
# -> {"vehicle_id": "...", "amount_cents": 300, "already_confirmed": true}

Invoke-RestMethod -Method Get "http://localhost:8080/fuel/vehicle/quota?vehicle_id=$vehicleId" `
  -Headers @{Authorization = "Bearer $ownerToken"}
# -> {"quota_cents": 500, "reserved_cents": 0, "used_cents": 300, "available_cents": 200}
```

Next: Stage 8 — owner dashboard

---

## Stage 8 — owner analytics (backend only) — DONE (2026-07-14)

Read-side aggregation only: no new money mechanics, no crypto, no new
persistence for money. Built entirely on Stage 1 (identity/auth), Stage 2
(ledger), Stage 3 (routing), Stage 4 (telemetry), Stage 5 (boarding), and
Stage 7 (fuel) — earlier code was only touched to add read routes/read-only
query helpers.

**CORE PRINCIPLE — reconciliation, enforced structurally and by test:** every
monetary figure returned by `internal/analytics` is a live `SUM()` over
`ledger_postings` (joined to `ledger_transactions`/`accounts`), never a
separate counter or cached tally — the same derivation every prior stage
already uses for a balance. Trip/passenger counts are `COUNT(DISTINCT
ledger_transactions.id)` over real `kind='fare'` transactions, not an
incremented counter anywhere. There is exactly one source of truth: the
ledger. The one documented exception is **fuel consumed**: Stage 7
deliberately keeps quota consumption OFF the ledger (funding a quota,
authorizing, and confirming a pump session post **zero** new
`ledger_postings` rows — its anti-bypass property). Consumption therefore
cannot be ledger-derived by construction; it's read from
`fuel_authorizations` (`status='confirmed'`), Stage 7's own real settlement
record. This is called out in `internal/analytics/models.go`'s package doc
comment, and revenue/fuel-allocated figures next to it stay ledger-derived
exactly like everything else.

Built:
- `backend/internal/analytics/` — the new analytics module:
  - `models.go` — response DTOs (`Summary`, `VehicleStat`, `DriverStat`,
    `RevenueVsFuel`/`RevenueVsFuelDay`, `LedgerEntry`/`LedgerPage`) plus the
    package doc comment carrying the reconciliation principle and SCOPE
    HONESTY (below) up front.
  - `daterange.go` — `parseDateRange(r)`, the one documented "today"/range
    boundary the brief requires. Anchored to a **fixed `Africa/Johannesburg`
    timezone** (`time.LoadLocation`, with a blank `_ "time/tzdata"` import so
    it resolves even on a dev machine with no system IANA tz database
    installed — this Windows box has none by default). `?from=`/`?to=`
    accept either `YYYY-MM-DD` (interpreted as midnight in that zone; for
    `to` specifically, bumped to midnight of the *next* day so a plain date
    includes that whole day) or a full RFC3339 timestamp used as-is. Missing
    `from` defaults to the start of today; missing `to` defaults to now —
    together the no-params default is exactly "today so far."
  - `repo.go` — every aggregation as SQL (`GROUP BY`, `SUM`, `COUNT`), not
    loaded-then-summed-in-Go, per the brief's performance guidance:
    `Summary` (owner_revenue/platform_fee/driver_earnings totals for the
    range + the CURRENT, non-range-bound fuel_account balance via
    `fuel.Repo.Balance` + range-bound fuel-allocated), `VehicleStatsForOwner`
    / `DriverStatsForOwner` (one grouped query each, keyed by the
    `vehicle_id`/`driver_user_id` every fare transaction's `metadata` already
    carries from Stage 2's `ChargeFare` — answers every vehicle's/driver's
    stats in one query instead of one query per vehicle/driver), three daily
    `date_trunc`-bucketed series queries for revenue-vs-fuel, and `Ledger`, a
    single `UNION ALL` CTE across fare/allocation/authorization sources with
    `ORDER BY ... LIMIT/OFFSET` done in SQL (plus a matching `COUNT(*)` query
    for the pagination total).
  - `handlers.go` — `Summary`, `Vehicles`, `Drivers`, `RevenueVsFuel`,
    `Ledger`. Every handler resolves the owner strictly from
    `identity.ClaimsFromContext` (the validated JWT), never from a request
    parameter — this is what makes cross-owner access structurally
    impossible rather than merely filtered client-side (see Scoping below).
- `backend/internal/identity/repo.go` — added the read-only list/lookup
  helpers this stage needed and Stage 1 never did:
  `ListVehiclesByOwnerUserID`, `GetActiveAssignmentByVehicleID` (the
  vehicle-keyed mirror of the existing driver-keyed
  `GetActiveVehicleAssignmentByDriverID`), `GetDriverByID`, and
  `ListDriversByOwnerUserID` (join `drivers` ⋈ active `vehicle_assignments`
  ⋈ `vehicles` on `owner_user_id`). No schema changes — pure additive query
  helpers over Stage 1's existing tables.
- `backend/internal/server/router.go`, `backend/cmd/server/main.go` — wired
  `analytics.NewRepo`/`analytics.NewHandlers` in; `NewRouter` gained an
  `analyticsHandlers` parameter (`health_test.go` updated to match, same
  pattern as every prior stage). Routes, all in the existing owner-only
  group (`identity.RequireAuth` + `identity.RequireRole(RoleOwner)`,
  established in Stage 7):
  - `GET /owner/summary`, `GET /owner/vehicles`, `GET /owner/drivers`,
    `GET /owner/revenue-vs-fuel`, `GET /owner/ledger` — all accept
    `?from=&to=`; `/owner/ledger` additionally accepts `?limit=&offset=`
    (default 50, capped at 200).

SCOPE HONESTY (per CLAUDE.md and the stage brief), stated in
`internal/analytics/models.go`'s package doc comment:
- No persisted historical snapshots or GPS tracks. Monetary/trip figures are
  computed live from timestamped ledger postings, so they're accurate for
  *any* date range. Live fleet status (`online`, `current_route_*`,
  `seats_available` in `/owner/vehicles`) reflects Stage 4's CURRENT
  in-memory `VehicleStateStore` — right now, not a recorded timeline. A
  vehicle that was online an hour ago but has since disconnected shows
  offline; there's no history log to answer "was it online at 3pm."
- "Today"/date bounds use one fixed documented timezone
  (`Africa/Johannesburg`) for the whole MVP, not per-owner/per-request
  timezone handling.

Decisions / deviations from the original plan:
- **Passenger volume equals trip count** in this MVP (`passenger_volume` in
  `Summary` is literally the same number as `trips`) — one fare charge is one
  commuter boarding one vehicle once (Stage 2/5's model has no multi-seat
  single fare concept), so there's no independent passenger-count signal to
  report. Reported as its own field anyway (rather than omitted) since the
  brief asked for it explicitly and a future multi-passenger fare model would
  give it a genuinely different value.
- **Per-vehicle/per-driver attribution reads the `vehicle_id`/
  `driver_user_id`/`owner_user_id` already embedded in each fare
  transaction's `metadata` jsonb** (written once, at charge time, by Stage
  2's `ChargeFare` — not something this stage added). This is *not* a
  deviation from "derive from the ledger": the money figures still come from
  `SUM(ledger_postings.amount_cents)`; metadata is only used to know *which*
  vehicle/driver/owner a given already-ledger-verified posting belongs to.
  Ownership-based scoping (see below) does NOT rely on this metadata for
  security — it filters by the owning `accounts.owner_user_id` column
  wherever an account is owner-owned (`owner_revenue`, `fuel_account`), which
  cannot be forged by a client. `driver_earnings` accounts belong to the
  driver, not the owner, so those *are* scoped via
  `metadata->>'owner_user_id'` (set server-side by `ChargeFare`, never
  client-supplied) — the driver-earnings query in `repo.go` documents this
  distinction inline.
- **`/owner/ledger`'s three-source `UNION ALL` is one CTE query with SQL-side
  `LIMIT`/`OFFSET`**, not three separate queries merged/paginated in Go —
  chosen for the brief's "write aggregations as SQL where sensible"
  guidance; a second matching `COUNT(*)` query supplies the pagination
  total (demo-scale, acceptable to run twice rather than adding a window
  function).
- **`GET /owner/vehicles`/`GET /owner/drivers` still do a handful of
  per-row lookups in Go** (driver name for a vehicle's assignment, telemetry
  state, fuel quota) rather than one giant join — the seeded/demo fleet size
  is small (Stage 1/7 precedent: "small dataset, just loop"), and the *money*
  aggregation (the part performance actually matters for) is the one part
  done as grouped SQL (`VehicleStatsForOwner`/`DriverStatsForOwner`).

Tests (`backend/internal/analytics/analytics_test.go`, against a real
Postgres, skips like every prior stage's integration tests if none is
reachable, driven through the real HTTP handlers behind
`identity.RequireAuth`+`RequireRole(RoleOwner)` with real bearer tokens):
- `TestReconciliation_SummaryMatchesLedgerSums` — the stage's core property:
  charges three fares + one fuel allocation, then asserts `/owner/summary`'s
  `revenue_cents`/`trips`/`fuel_balance_cents` exactly equal SUM/COUNT
  queries computed **independently** in the test (not by calling the same
  repo code the handler uses) — no figure drifts from the ledger.
- `TestSplitConsistency_PlatformDriverOwnerSumToFareTotal` — the brief's
  explicit second property: `revenue_cents + platform_fees_cents +
  driver_earnings_cents` for the range equals the total fares charged,
  matching Stage 2's fare split exactly.
- `TestScoping_OwnerCannotSeeAnotherOwnersData` — two owners, each with their
  own vehicle/driver/fare: asserts owner1's `/owner/summary` revenue is
  exactly their own fare's owner share (not owner2's), and that
  `/owner/vehicles`, `/owner/drivers`, `/owner/ledger` for owner1 never
  contain owner2's vehicle/driver ids.
- `TestDateRange_RespectsFromTo` — a fare charged "now" is invisible to a
  `?from=&to=` window five days in the future (zero revenue/trips) and
  visible in a window covering today.
- `TestEmptyState_NoActivityReturnsCleanZeros` — a freshly-registered owner
  with no vehicles/fares/fuel activity gets `200` with every figure `0` and
  every list empty, not an error.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` changes — analytics needed no new
dependencies). Full test suite (`go test ./...`), including the new
analytics tests, passes against a live Postgres, and `go test -race ./...`
(MSYS2 `ucrt64` gcc toolchain, same as Stages 4-7) passes cleanly across
every package with **no data races detected** and no regressions in any
prior stage's tests. End-to-end verified by hand against the seeded dev
data — see the PowerShell walkthrough below (seed → charge fares across both
seeded vehicles → allocate fuel → all five `/owner/*` endpoints as the
seeded owner → registered a second owner and confirmed they see clean empty
data, not the first owner's).

### PowerShell walkthrough

Assumes the Docker Postgres is running and `cmd/server`/`cmd/seed` use the
default `localhost:5432` (stop the native Windows Postgres service first if
it's shadowing that port — see CLAUDE.md Stage 0 note). Run from `backend/`.

```powershell
# 1. Seed dev data (idempotent) and start the server in another terminal.
go run ./cmd/seed
go run ./cmd/server
```

```powershell
# 2. In a second terminal: log in as the seeded owner and both drivers, and
#    charge a few fares across BOTH seeded vehicles so /owner/vehicles and
#    /owner/drivers each have two rows to show. Substitute ids cmd/seed
#    printed if they differ from a prior run.

$ownerLogin = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000001","password":"Owner123!"}'
$ownerToken = $ownerLogin.token

$driver1Login = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000002","password":"Driver123!"}'
$driver1Token = $driver1Login.token

$driver2Login = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000003","password":"Driver123!"}'
$driver2Token = $driver2Login.token

$commuter1 = (Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000004","password":"Commuter123!"}').user_id
$commuter2 = (Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000005","password":"Commuter123!"}').user_id

# vehicleId1 = CA123456's id, vehicleId2 = CA654321's id, both printed by cmd/seed
$vehicleId1 = "<paste CA123456's id from cmd/seed output>"
$vehicleId2 = "<paste CA654321's id from cmd/seed output>"

Invoke-RestMethod -Method Post http://localhost:8080/fare/charge `
  -Headers @{Authorization = "Bearer $driver1Token"} -ContentType "application/json" `
  -Body (@{commuter_id=$commuter1; vehicle_id=$vehicleId1; fare_cents=3500; idempotency_key=[guid]::NewGuid().ToString()} | ConvertTo-Json)
Invoke-RestMethod -Method Post http://localhost:8080/fare/charge `
  -Headers @{Authorization = "Bearer $driver1Token"} -ContentType "application/json" `
  -Body (@{commuter_id=$commuter1; vehicle_id=$vehicleId1; fare_cents=1200; idempotency_key=[guid]::NewGuid().ToString()} | ConvertTo-Json)
Invoke-RestMethod -Method Post http://localhost:8080/fare/charge `
  -Headers @{Authorization = "Bearer $driver2Token"} -ContentType "application/json" `
  -Body (@{commuter_id=$commuter2; vehicle_id=$vehicleId2; fare_cents=900; idempotency_key=[guid]::NewGuid().ToString()} | ConvertTo-Json)
```

```powershell
# 3. Owner withholds fuel from revenue (Stage 7), so the summary/revenue-vs-
#    fuel figures below have something non-zero to show on the fuel side.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/allocate `
  -Headers @{Authorization = "Bearer $ownerToken"}
```

```powershell
# 4. Call every Stage 8 endpoint as the owner.
Invoke-RestMethod -Method Get http://localhost:8080/owner/summary `
  -Headers @{Authorization = "Bearer $ownerToken"}
Invoke-RestMethod -Method Get http://localhost:8080/owner/vehicles `
  -Headers @{Authorization = "Bearer $ownerToken"}
Invoke-RestMethod -Method Get http://localhost:8080/owner/drivers `
  -Headers @{Authorization = "Bearer $ownerToken"}
Invoke-RestMethod -Method Get http://localhost:8080/owner/revenue-vs-fuel `
  -Headers @{Authorization = "Bearer $ownerToken"}
Invoke-RestMethod -Method Get http://localhost:8080/owner/ledger `
  -Headers @{Authorization = "Bearer $ownerToken"}
```

```powershell
# 5. Register and log in as a SECOND, unrelated owner and confirm they see
#    only their own (empty) data — not the first owner's.
Invoke-RestMethod -Method Post http://localhost:8080/auth/register `
  -ContentType "application/json" -Body '{"phone":"+27820099999","password":"Owner2Pass!","role":"owner"}'
$owner2Login = Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820099999","password":"Owner2Pass!"}'
$owner2Token = $owner2Login.token

Invoke-RestMethod -Method Get http://localhost:8080/owner/summary `
  -Headers @{Authorization = "Bearer $owner2Token"}
# -> every figure 0, not an error

Invoke-RestMethod -Method Get http://localhost:8080/owner/vehicles `
  -Headers @{Authorization = "Bearer $owner2Token"}
# -> {"vehicles": []} — NOT owner 1's CA123456/CA654321

Invoke-RestMethod -Method Get http://localhost:8080/owner/ledger `
  -Headers @{Authorization = "Bearer $owner2Token"}
# -> {"entries": null, "total": 0, ...} — NOT owner 1's fare/allocation history
```

Next: Stage 9 — frontends (commuter, driver, owner apps)

---

## Stage 9a — driver app — DONE (2026-07-14)

Frontend only — no backend logic changes. Built entirely on Stages 1-8's
existing REST/WS surface; the one backend touch was adding dev CORS (see
below), which every later frontend stage (9b commuter, 9c owner) will also
rely on.

**Stack**: Vite + React 18 + TypeScript (strict), Tailwind CSS, `html5-qrcode`
for camera QR scanning. No state library beyond React hooks + one
`AuthContext` — the app is small enough that Redux/Zustand/etc. would be pure
overhead, not a real need.

Built:
- `apps/driver/` — a self-contained workspace (own `package.json`,
  `vite.config.ts`, `tsconfig.json`, Tailwind/PostCSS config). Dev server on
  port **5174** (the backend already owns 8080). `VITE_API_BASE_URL` (default
  `http://localhost:8080`) in `.env`/`.env.example` — Vite only exposes
  `VITE_`-prefixed vars to client code.
- `src/api/client.ts` — a small typed `fetch` wrapper (`request<T>`), one
  `ApiError` class carrying the HTTP status + the backend's `{"error": "..."}`
  message, a module-level `authToken` set by `AuthContext` on login/logout
  (simpler than threading the token through every call site), and
  `wsBaseUrl()` (swaps `http`→`ws` on the same configured base URL) since
  `/ws/driver` lives on the same origin/port as the REST API.
- `src/types.ts` — wire types hand-mirrored from the backend's actual JSON
  responses (`internal/*/handlers.go`) — there's no OpenAPI/codegen in this
  repo yet, so these are kept 1:1 with the Go response structs by hand.
- `src/context/AuthContext.tsx` — login (`POST /auth/login`, rejects a
  non-driver role client-side before ever showing the dashboard), logout,
  token persistence. **Token storage: `sessionStorage`, not `localStorage`**
  — still plaintext-JS-readable (no hardened secure-storage exists in a
  browser), but at least clears on tab close rather than persisting
  indefinitely; documented as a deliberate middle ground for a dev MVP with
  no refresh-token flow, not a hardened choice, per the brief's explicit
  instruction not to use `localStorage` for anything security-sensitive.
- `src/hooks/useGeolocation.ts` — wraps `navigator.geolocation.watchPosition`
  behind an `enabled` flag; reports a distinct status per outcome (`watching`
  / `denied` / `unsupported` / `error`) rather than a single boolean, so the
  UI can show *why* location isn't flowing, not just that it isn't. Doc
  comment flags the secure-context requirement (below).
- `src/hooks/useDriverSocket.ts` — owns the single bidirectional `/ws/driver`
  connection for the session: sends `{lat,lng}` / seat messages, receives
  pushed `stop_request` alerts. **Reconnects with backoff** (1s/2s/5s/8s)
  while the driver is still toggled online, rather than silently going dark
  on a dropped connection — exposes a `status` (`connecting` /
  `open` / `reconnecting` / `closed`) the dashboard renders directly. The
  JWT is passed as a `?token=` query param (per `telemetry.bearerToken`'s
  documented fallback — browsers' `WebSocket` constructor cannot set custom
  handshake headers).
- Screens (`src/screens/`), wired together by `src/DriverApp.tsx` (all
  cross-screen state — routes, socket, geolocation, seats, alerts — lives
  here; screens are presentational, given props):
  1. **Login** — phone + password, clean error display on bad credentials.
  2. **Dashboard (Home)** — route picker (`GET /routes`) → selecting a route
     immediately opens `/ws/driver` and starts streaming position (no
     separate "confirm" step, matching Stage 4's "going online means online
     on a route" model). Shows connection status and geolocation status as
     pills, plus the last known lat/lng.
  3. **Scan** — the hero action: camera QR scan (`html5-qrcode`,
     `facingMode: "environment"`) or a manual paste-token fallback text area,
     either path calling `POST /boarding/scan`. The receipt view visually
     distinguishes a **fresh charge** (green, "Fare charged") from an
     **idempotent replay** (amber, "Already charged (replay)" with an
     explanation) — both cases the API can return, per Stage 5.
  4. **Seats & earnings** — ± buttons call `POST /telemetry/seats` with
     `{delta}`; on first connecting, a `{delta: 0}` no-op call is used purely
     to *read* the vehicle's current seat state (there's no dedicated GET for
     a driver's own vehicle). Earnings shows the `driver_earnings` balance
     via `GET /wallet/balance` — **there is no driver-scoped trips/earnings
     breakdown endpoint yet** (Stage 8's richer stats are owner-only, under
     `/owner/*`), so this screen is balance-only; flagged in the README
     rather than faked.
  5. **Alerts** — lists `stop_request` messages pushed over the same
     `/ws/driver` connection (Stage 6); acknowledging one calls
     `POST /stops/request/{id}/ack` and removes it from the list. A badge
     count on the bottom-nav tab surfaces unread alerts from any screen.
- Bottom tab nav (`src/components/BottomNav.tsx`) — mobile-first, thumb-reach
  navigation between the four screens; Tailwind throughout for a demo-ready
  look rather than an unstyled wireframe.

**Backend change (the only one this stage made)**:
`backend/internal/server/router.go` gained a `devCORS` middleware — reflects
the request's `Origin`, allows `GET/POST/PUT/PATCH/DELETE/OPTIONS`, allows
the `Authorization`/`Content-Type` headers, and answers `OPTIONS` preflights
with a bare 204. Explicitly commented as **dev-only, allow-all**: this MVP
has no cloud/production deployment yet, so there's no real origin allowlist
to enforce against; tightening this is flagged as needed before any
non-local deployment. No other backend files changed.

SCOPE HONESTY (per CLAUDE.md and the stage brief):
- **Geolocation and camera both require a secure context** (`https://`, or
  the browser's special-cased `http://localhost`). This app works as-is in
  dev on a desktop browser at `http://localhost:5174`. It will **not** work
  testing on a real phone over `http://<lan-ip>:5174` — most mobile browsers
  silently deny both permissions over plain HTTP on a non-localhost host.
  On-device testing needs an HTTPS tunnel or a real certificate, neither set
  up here — called out in `apps/driver/README.md` as a known gap for a later
  stage, not solved in this one.
- The seat "peek" via `{delta: 0}` is a documented workaround for the lack of
  a dedicated "my vehicle's current state" GET endpoint for drivers — not a
  new backend capability, just reusing the existing seats endpoint's return
  value.

Verified: `go build ./...` (backend, after the CORS change), `npm install`,
`npx tsc --noEmit`, and `npm run build` (frontend) all pass cleanly. Ran
Postgres + `cmd/server` + `npm run dev` together and confirmed: the Vite dev
server serves `http://localhost:5174`, and a cross-origin `OPTIONS`/
`POST /auth/login` sent with `Origin: http://localhost:5174` against the
live backend succeeds with the new CORS headers and returns a real driver
JWT (seeded driver `+27820000002` / `Driver123!`) — proving the frontend can
actually reach the backend cross-origin.

### Design revisit (2026-07-14) — applied the `frontend-design` skill

The first pass (above) was functionally complete but visually generic —
dark-slate background with a single emerald accent, one of the
skill-documented default "AI-generated" looks. Redid the whole visual layer
(no logic/data-flow changes) around one concrete subject: the hand-lettered
destination board a real minibus taxi driver tapes to the windscreen.

- New token system in `tailwind.config.js`: `board`/`ink` (destination-board
  cream + marker lettering), `rank` (rank/curb-paint yellow, primary action),
  `taxi` (livery blue), `brake` (brake-light red, alerts/replays only), `tar`
  (warm near-black backdrop, not cool slate). No webfont fetch — display
  lettering uses a heavy system sans at large scale/tight tracking rather
  than an exotic typeface, keeping the app dependency-free/offline-friendly.
- Signature components in `src/index.css` (`@layer components`): `.board`
  (the destination-board card — reused for login, the route/status header,
  and the earnings readout), `.ticket`/`.stamp` (the boarding-scan receipt:
  a torn till-slip with a rotated rubber-stamp verdict — taxi-blue "Paid" for
  a fresh charge, brake-red "Already Paid" for an idempotent replay — a
  direct visual answer to the one thing that screen has to communicate),
  `.led` (dashboard instrument-light status indicators, replacing soft pill
  badges).
- Every screen (`Login`, `Dashboard`, `Scan`, `Seats`, `Alerts`) and both
  shared components (`StatusPill`, `BottomNav`) restyled to the new system;
  `SeatsScreen` gained a seat grid (one square per physical seat, filled vs.
  dim) instead of a bare fraction.
- Full design rationale documented in `apps/driver/README.md`'s new "Design
  direction" section.

Verified visually, not just by build: installed `playwright` in the scratch
dir (pointed at the machine's already-cached Chromium, since a fresh
`playwright install` wasn't available) and drove the real running app —
login, route selection going online, all four tabs, and a full boarding-pass
issue → scan → receipt → replay-scan round trip against the live backend (as
a real seeded commuter + driver) — capturing a screenshot at every step to
self-critique against the skill's process instead of eyeballing rendered
JSX. Confirmed the fresh-charge vs. replay stamp distinction renders correctly and
the destination-board motif holds up across the online/offline states.
`npx tsc --noEmit` and `npm run build` re-verified clean after the restyle.

Next: Stage 9b — commuter app

---

## Stage 9b-i — commuter app: map, route search, live vehicles — DONE (2026-07-14)

Frontend only, no backend logic changes at all — reused the dev CORS added in
Stage 9a as-is (it reflects any `Origin`, so a third dev-server port needed no
backend change). First half of the commuter app: sign-in, the live map, route
search, and route detail. Wallet, boarding-pass generation, and the
active-trip screen are Stage 9b-ii, not built here.

**Stack**: Vite + React 18 + TypeScript (strict), Tailwind CSS,
`react-leaflet` + `leaflet` (OpenStreetMap tiles) for the live map. Same
`AuthContext` (JWT in memory + `sessionStorage`) and typed-`fetch`-wrapper
API-client pattern as the driver app (Stage 9a) — no state library, the app
is small enough that one wouldn't earn its keep.

Built:
- `apps/commuter/` — a self-contained workspace (own `package.json`,
  `vite.config.ts`, Tailwind/PostCSS config). Dev server on port **5175**
  (backend 8080, driver app 5174). `VITE_API_BASE_URL` in `.env`/`.env.example`.
- `src/api/client.ts`, `src/types.ts` — same typed-`fetch` pattern and
  hand-mirrored wire types as the driver app; added `searchRoutes`,
  `getRoute` and the `GET /ws/commuter` message union
  (`CommuterSnapshotMessage`/`CommuterUpdateMessage`/`CommuterOfflineMessage`).
- `src/context/AuthContext.tsx` — identical login/logout/token-persistence
  shape to the driver app's, checking for `role === "commuter"` instead of
  `"driver"`; same `sessionStorage`-not-`localStorage` trade-off, documented
  the same way.
- `src/hooks/useRoutesData.ts` — **there is no `GET /stops` endpoint**
  (Stage 3 only ever needed a stop by id or exact name for `/routes/search`).
  Rather than add a backend endpoint for a frontend-only stage, this hook
  fetches every route (`GET /routes`) and then every route's detail
  (`GET /routes/{id}`) once on load and de-duplicates the stops named in each
  route's legs into a single sorted stop list — good enough at this route
  count; would need a real endpoint if the route graph grew large. The same
  fetched `RouteDetail` map is reused directly by the Routes screen (no
  second fetch needed to view a route's stops).
- `src/hooks/useRouteVehicles.ts` — the receive-only counterpart to the
  driver app's `useDriverSocket`: owns `GET /ws/commuter?route_id=<id>`,
  applies `snapshot`/`update`/`offline` events into a `Map<vehicleId,
  VehicleView>`, and reconnects with the same 1s/2s/5s/8s backoff while a
  route stays selected. No `enabled` gate (unlike the driver socket) — the
  commuter endpoint is intentionally public (Stage 4), so watching starts the
  instant a route is picked, logged in or not.
- Screens (`src/screens/`), wired by `src/CommuterApp.tsx`:
  1. **Login** — phone + password, clean error on bad credentials.
  2. **Map (`Live` tab, the hero screen)** — a route selector (`GET /routes`)
     plus a Leaflet map centered on Cape Town. Selected route opens
     `useRouteVehicles`; markers are custom `.vehicle-marker` chips (not
     Leaflet's default pin) labelled with `seats_available`. Three honest
     states, no broken/blank map for any of them: **no route picked** yet
     (prompt to pick one), **no vehicles online** on the picked route (empty
     state, not a blank map), and **live** (real markers, moving as `update`
     events arrive). A dropped socket shows a "Reconnecting" flap and
     re-subscribes automatically.
  3. **Search** — origin/destination pickers built from `useRoutesData`'s
     stop list (mutually exclusive — can't pick the same stop for both),
     `GET /routes/search?from=&to=`. Renders the ordered stop sequence across
     all segments with a "⟳ Transfer at `<stop>`" marker between segments for
     multi-hop results, each segment's own route name + fare, and the total
     fixed fare in Rands. A 404 (no path) is caught by status code and shown
     as a clean "No route found" message — not treated as a generic error.
  4. **Routes** — a tappable list of every route; tapping shows its ordered
     stops with each leg's fare and the full-route fare total (from the
     already-fetched `RouteDetail` cache, no extra request). Reachable
     directly from its own tab, or by tapping a segment in a Search result
     (`onViewRoute` switches to this tab with that route pre-selected).
- `src/components/BottomNav.tsx` — three tabs (`Live`/`Search`/`Routes`),
  same mobile-first bottom-tab-bar shape as the driver app's.

SCOPE HONESTY (per CLAUDE.md and the stage brief):
- **Leaflet's tiles are fetched from OpenStreetMap over the internet** — the
  one online dependency in an otherwise fully-local app, called out both in
  the map screen's own footer text and in the README. No connection means a
  blank map (the rest of the UI still renders).
- **The stop list is derived client-side, not served by a dedicated
  endpoint** (see `useRoutesData.ts` above) — an explicit, documented
  workaround for a gap in Stage 3's API surface, not a new backend
  capability.
- No wallet balance, no boarding-pass QR generation/display, no active-trip
  screen — all explicitly Stage 9b-ii per the brief.

Decisions / deviations from the original plan:
- **Search's origin/destination selects disable whichever stop is currently
  chosen in the other field**, rather than allowing (then rejecting) a
  same-stop search — a small UX guard the brief didn't ask for explicitly
  but that avoids a pointless round-trip to the 400 the backend would
  otherwise return for identical origin/destination.
- **Vehicle markers use a custom `L.divIcon`, not Leaflet's default marker
  image.** `react-leaflet`'s default marker asset path famously breaks under
  Vite bundling; a divIcon sidesteps that entirely and lets the marker share
  the app's own visual language (a taxi-board chip with a seat count) instead
  of a generic map pin.
- **The Routes tab's detail view reuses the `RouteDetail` map `useRoutesData`
  already fetched for stop aggregation**, rather than re-fetching
  `GET /routes/{id}` on tap — that data was already pulled down whole for the
  stop list, so re-fetching it would be pure waste.

Verified: `npm install`, `npx tsc --noEmit`, and `npm run build` all pass
cleanly (no `go build`/backend changes to verify — this stage touched no Go
code). Ran Postgres + `cmd/server` + `npm run dev` together and drove the
real app end-to-end with Playwright (Chromium, same cached-browser approach
as Stage 9a): logged in as the seeded commuter (`+27820000004` /
`Commuter123!`); confirmed the live map's clean "no vehicles" empty state on
a route with no driver online; brought a seeded driver online on a route via
`cmd/wsdriver` and confirmed a real marker with a live seat count appeared on
the map through the actual snapshot/update WebSocket flow (not mocked); ran a
direct search (Cape Town Station → Khayelitsha Town Centre, R35.00), a
one-transfer search (Khayelitsha Town Centre → Wynberg, transfer at Athlone,
R31.00 total), and a genuine no-path search (Khayelitsha Town Centre →
Muizenberg) that rendered the clean "No route found" state rather than an
error screen; and opened a route's detail view (ordered stops + per-leg
fares) both from a search result segment and from the Routes tab directly.

Next: Stage 9b-ii — commuter app: wallet, boarding-pass generation, active trip

---

## Stage 9b-ii — commuter app: wallet, boarding pass, active trip — DONE (2026-07-14)

Frontend only, no backend changes — extends `apps/commuter` (Stage 9b-i),
reusing its `AuthContext`, typed `api` client, design tokens/components
(`.board`, `.ticket`, `.tape`, `.flap`), and the `useRouteVehicles` hook. This
is the stage that **closes the core loop**: a commuter tops up a wallet and
generates the HMAC boarding-pass QR (Stage 5) that the driver app
(`apps/driver`, Stage 9a) scans via `POST /boarding/scan`, charging the same
Stage 2 ledger this app's wallet reads.

Built:
- `src/types.ts`, `src/api/client.ts` — added `BalanceResponse`,
  `TopupResponse`, `IssuePassResponse`, `RequestStopResponse` wire types and
  `api.getBalance`/`api.topup`/`api.issuePass`/`api.requestStop`, following
  the existing hand-mirrored-from-Go-handlers pattern.
- `src/hooks/useCountdown.ts` — ticks every 250ms toward an RFC3339
  `expiresAt`, returning `{remainingMs, expired, label}` (`m:ss`). The one new
  hook this stage needed — a short-TTL boarding pass (Stage 5, ~3 minutes)
  needs a visibly live countdown, not a static timestamp.
- `src/screens/WalletScreen.tsx` — `GET /wallet/balance` in Rands, a demo
  top-up (`POST /wallet/topup`: R20/R50/R100 presets or a custom Rand amount,
  converted to cents), explicitly labelled **"Demo top-up only... no real
  payment gateway"** per CLAUDE.md's non-negotiable. A session-local list of
  top-ups made from this device is shown underneath **SCOPE HONESTY**: there
  is no backend endpoint exposing a commuter's own transaction history (only
  `/owner/ledger`, Stage 8, owner-only, sees the full ledger), so this list is
  explicitly a client-side session log, not ledger-derived, and says so
  on-screen. A "Refresh" button re-fetches the balance, useful right after a
  driver scans your pass elsewhere (this screen has no push channel).
- `src/screens/BoardScreen.tsx` — the stage's signature screen, two states:
  1. **Trip selection**: route dropdown, then from/to stop dropdowns
     constrained to that route's stops in physical sequence order (derived
     from `RouteDetail.legs` the same way `RoutesScreen` already does), the
     "to" select disabling every stop at or before the chosen "from" — this
     mirrors the increasing-sequence constraint `routing.FareForSegment`
     (Stage 3/5) enforces server-side, so a submitted pair can never 404.
     "Generate boarding pass" calls `POST /boarding/pass`.
  2. **The boarding pass / active trip view**, once issued:
     - **The QR is the hero**: `qrcode.react`'s `QRCodeSVG` renders
       `pass_token` large and centered in a white card, matching the driver
       app Scan screen's expectation of a camera-scannable code.
     - A live countdown (`useCountdown`) to `expires_at` with a rotated
       rubber-stamp verdict — `.stamp-live` (transit teal, "Valid") while
       counting down, `.stamp-expired` (flag red, "Expired") once it hits
       zero, at which point the QR is replaced by a "Generate new pass"
       action for the same trip (state persists — only the issued pass is
       cleared, not the route/stop selection).
     - Fare in Rands and the route/from/to in plain language.
     - A `<details>` "No camera? Show raw token" disclosure exposing the raw
       `pass_token` string plus a clipboard-copy button — the fallback for
       desktop/dev testing without a camera, pasted into the driver app's
       manual-entry field exactly like Stage 9a's Scan screen expects.
     - **Active trip (light MVP)**: a trip-status summary plus, if any
       vehicles are currently online on the pass's route, their live seat
       counts — reusing `useRouteVehicles`/`GET /ws/commuter` verbatim (no new
       hook). **SCOPE HONESTY**: explicitly not a single-vehicle tracker —
       telemetry (Stage 4) has no concept of "the vehicle assigned to this
       commuter's specific trip," so this is an honest route-wide view, not a
       faked "your driver is 200m away."
     - **Request-a-pickup**: a small widget (stop select defaulting to the
       route's first stop, "Request" button) calling `POST /stops/request`
       (Stage 6's commuter-side counterpart to the driver app's alert
       receipt) and rendering the real `driver_available` result — included
       per the brief's "if it fits cleanly," kept to one card rather than a
       separate tab/screen.
- `src/index.css` — added `.stamp`/`.stamp-live`/`.stamp-expired`, the
  commuter app's own rubber-stamp verdict styling (transit teal / flag red),
  parallel to the driver app's Paid/Already-Paid stamp but reporting pass
  validity instead of a charge outcome, since the commuter never sees the
  charge itself (only the driver app does, on scan).
- `src/components/BottomNav.tsx` — grew from 3 to 5 tabs (`Live`/`Search`/
  `Routes`/`Wallet`/`Board`), grid updated to `grid-cols-5`.
- `src/CommuterApp.tsx` — wired `WalletScreen`/`BoardScreen` into the two new
  tabs.
- `package.json` — added `qrcode.react` (a maintained, TypeScript-typed QR
  rendering library) as the only new dependency this stage needed.

Decisions / deviations from the original plan:
- **A boarding pass is scoped to one route chosen up front, not derived from
  a Search-tab result.** The brief's "reuse the stop-selection UI from 9b-i's
  search" was interpreted as reusing the *pattern* (styled dropdowns
  populated from real route/stop data) rather than literally wiring the
  Search screen's cross-route multi-segment flow into pass issuance —
  `POST /boarding/pass` only ever prices a single route's segment
  (`routing.FareForSegment`), so a route-first, then-constrained-stops
  selector is the more direct match for what the endpoint actually accepts,
  and avoids the from/to constraint being checked in two different UI shapes.
- **Bug found and fixed during Playwright verification**: the request-a-stop
  select showed a default stop (falling back to the route's first stop when
  no explicit selection had been made), but the "Request" button read the
  underlying (still-empty) state variable directly and silently no-opped.
  Fixed by resolving the effective stop id (explicit selection, or the same
  fallback the select displays) at the call site and passing it into
  `requestPickup` as an argument, rather than trusting a state variable the
  UI's own default-display logic had already diverged from. Caught by
  screenshotting the widget after clicking "Request" and seeing no result
  text — a reminder that a value shown in a controlled input isn't
  necessarily the value React state actually holds.
- **`.env`/dependency housekeeping**: no `.env`/CORS changes needed — Stage
  9a's dev-CORS (reflects any `Origin`) already covers this app's existing
  port 5175.

Verified: `npm install`, `npx tsc --noEmit`, and `npm run build` all pass
cleanly. End-to-end verified against the live backend with Playwright
(`playwright-core` pointed at the machine's cached Chromium build, since a
fresh `npx playwright install` wasn't available in this environment) — see
`apps/commuter/README.md`'s "Verified" section for the full walkthrough and
exact numbers. Summary: brought seeded driver 1's vehicle online on "Cape
Town CBD - Bellville" via `cmd/wsdriver`; in the real commuter app, screenshotted
the wallet balance before/after a real R100 top-up (R3963.00 → R4063.00);
generated a real boarding pass for Cape Town Station → Bellville Station
(R11.00) and screenshotted the rendered QR, a ticking countdown (2:59 → 2:57
across two screenshots), and the "Valid" stamp; **closed the loop** by taking
that exact `pass_token` and `POST`ing it to `/boarding/scan` as the seeded
driver — got back a fresh charge (`replayed:false`, fare 1100, split
110/275/715, seats 16→15) and confirmed the commuter's wallet balance dropped
by exactly the fare (R4063.00 → R4052.00), verified both via the API directly
and by reloading the commuter app's Wallet screen and screenshotting the same
new balance rendered in the real UI; and exercised request-a-stop end-to-end,
confirming the real `driver_available:true` result rendered in the UI after
the fix above.

Next: Stage 9c — owner dashboard

---

## Stage 9c — owner dashboard — DONE (2026-07-14)

Frontend only, no backend changes — a new self-contained workspace,
`apps/owner`, built entirely on Stage 8's read-only `/owner/*` analytics
endpoints (`/owner/summary`, `/owner/vehicles`, `/owner/drivers`,
`/owner/revenue-vs-fuel`, `/owner/ledger`), reusing Stage 9a/9b's
`AuthContext`/typed-`fetch`-client pattern and the existing dev CORS
unchanged (it reflects any `Origin`, so a fourth dev-server port needed no
backend change).

**Stack**: Vite + React 18 + TypeScript (strict), Tailwind CSS, **Recharts**
(new dependency — the one chart-capable library needed, not present in the
driver/commuter apps). No state library. Dev server on port **5176**
(backend 8080, driver 5174, commuter 5175).

**The one deliberate difference from the other two frontends**: this app is
**desktop-first and data-dense**, not phone/one-handed-first — an owner
reviews their business on a laptop, not at a rank. A persistent left
`Sidebar` (Overview / Revenue vs Fuel / Fleet / Drivers / Ledger) replaces
the other apps' bottom-tab bar; content is a wide grid of stat cards, a
chart, and ruled tables with tabular-numeral money columns, not a single
scrollable phone-width card stack. No camera, GPS, WebSocket, or QR — this
app only ever reads `GET /owner/*`.

Built:
- `apps/owner/` scaffold: own `package.json`, `vite.config.ts` (port 5176),
  `tsconfig.json`/`tsconfig.node.json`, Tailwind/PostCSS config,
  `.env`/`.env.example` (`VITE_API_BASE_URL`), matching the driver/commuter
  apps' file shapes exactly.
- `src/types.ts` — wire types hand-mirrored from
  `internal/analytics/{models,handlers}.go`'s actual JSON: `Summary`,
  `VehiclesResponse`/`VehicleStat`, `DriversResponse`/`DriverStat`,
  `RevenueVsFuel`/`RevenueVsFuelDay`, `LedgerPage`/`LedgerEntry`.
- `src/api/client.ts` — the same typed-`fetch`-wrapper/`ApiError` pattern as
  the other two apps, plus `getSummary`/`getVehicles`/`getDrivers`/
  `getRevenueVsFuel`/`getLedger`, all taking an optional `{from, to}` (and
  `getLedger` additionally `{limit, offset}`) and building the query string.
  **Never sends an owner id as a parameter** — every `/owner/*` call is
  scoped server-side by the caller's own JWT (`ownerFromContext` in
  `internal/analytics/handlers.go`), exactly as the brief requires.
- `src/context/AuthContext.tsx` — identical shape to the driver/commuter
  apps', checking `role === "owner"` instead; same
  `sessionStorage`-not-`localStorage` trade-off, documented the same way.
- `src/components/`:
  - `Sidebar.tsx` — the desktop-first vertical nav rail (five tabs, a
    logout link, and a `.stamp-reconciled` "✓ Ledger-reconciled" badge —
    this app's one design callout of the integrity note below).
  - `DateRangePicker.tsx` — Today / Last 7 days / Last 30 days / Custom
    (two date inputs). Preset math is done client-side in the browser's
    local calendar and is **only ever used to choose a `from=` value to
    send** — every screen displays the range the API response itself
    echoes back (`ActiveRangeNote.tsx`), never the picker's own guess, so a
    client/server timezone mismatch can never show as a false figure.
  - `StatCard.tsx` — one headline figure per card; documented in its own
    comment as rendering the API value verbatim.
  - `ActiveRangeNote.tsx` — echoes the response's own `from`/`to`, captioned
    "(Africa/Johannesburg time, per the backend's date-range boundary)" —
    the one documented timezone fact from Stage 8's
    `internal/analytics/daterange.go`, restated here rather than
    re-derived.
- `src/hooks/useRangeData.ts` — one generic `{data, loading, error}` hook
  shared by four of the five screens (re-fetches on `range.from`/`range.to`
  change); the Ledger screen manages its own fetch loop since it also needs
  `offset` pagination state.
- `src/format.ts` — `formatRand` (cents → `"R1,234.56"`), `formatDate(Time)`
  — the **only** transform ever applied to a monetary figure in this app:
  unit conversion + display formatting, never recomputation (see the
  integrity note below).
- Screens (`src/screens/`), wired by `src/OwnerApp.tsx` (the date range is
  lifted once here, not duplicated per screen, so every screen genuinely
  shares one control):
  1. **Login** — phone + password; rejects a non-owner role client-side.
     Seeded owner: `+27820000001` / `Owner123!`.
  2. **Overview** (`GET /owner/summary`) — stat cards: revenue, trips,
     passenger volume, platform fees, driver earnings paid, fuel account
     balance, fuel allocated for the range.
  3. **Revenue vs Fuel** (`GET /owner/revenue-vs-fuel`) — the dashboard's
     signature view: headline totals plus a **Recharts `ComposedChart`**
     (grouped bars for revenue vs fuel allocated per day, a dashed line for
     fuel consumed). Y axis always starts at zero (`domain={[0, "auto"]}`)
     and is tick-labelled in Rands via `formatRand`, not raw cents or an
     abbreviated/truncated scale — the brief's "honestly scaled" requirement
     taken literally. A "fuel share of revenue" percentage is computed
     client-side for display, captioned as such, sitting right next to the
     two unmodified source figures it divides.
  4. **Fleet** (`GET /owner/vehicles`) — one row per vehicle: assigned
     driver, live online/offline + current route (Stage 4 telemetry, right
     now — not a historical log), seats, trips/revenue for the range, fuel
     quota (available/total).
  5. **Drivers** (`GET /owner/drivers`) — one row per driver: assigned
     vehicle, live online status, trips/earnings for the range.
  6. **Ledger** (`GET /owner/ledger`) — the transparency/anti-skimming view
     made visible: a paginated (`limit`/`offset`, Previous/Next, "Showing
     X–Y of Total"), chronological table unioning fare transactions (with
     the owner/driver/platform split spelled out per row), fuel
     allocations, and fuel-pump authorizations — the exact three sources
     `internal/analytics/repo.go`'s `Ledger` CTE unions, tagged by
     `entry_type` so the split stays visible.
- `src/index.css`, `tailwind.config.js` — the design system (see "Design
  direction" below): `paper`/`card`/`ink` (a cooler, calmer backdrop than
  the commuter app's warm `dawn`), `brass` (a desaturated accent relative
  of `rank`/`marigold`), `signal`/`alert` (online/offline,
  positive/negative), `.ledger-card` (flat ruled-cardstock, the same
  lineage as the other apps' `.board` but dense rather than
  taped-and-tilted), `.stamp-reconciled`, `table.ledger-table` (ruled rows,
  `.num` tabular-numeral cells).

**INTEGRITY NOTE (per the stage brief, stated in README.md and inline
comments)**: every monetary figure this dashboard shows is rendered
directly from a Stage 8 `/owner/*` response field — the app displays them,
it never recomputes or "adjusts" them. `formatRand()` only converts cents
to a display string. The one computed-for-display value anywhere in the
app (Revenue vs Fuel's "fuel share of revenue" ratio) is explicitly
captioned as derived-for-display-only, sitting beside its two unmodified
source figures so it can never be mistaken for a third ledger figure.

Decisions / deviations from the original plan:
- **Bug found and fixed during Playwright verification**:
  `internal/analytics/repo.go`'s `Ledger` declares `var entries
  []LedgerEntry` (a nil slice) rather than `make([]LedgerEntry, 0, ...)`
  the way `Vehicles`/`Drivers` do — so a date range with zero ledger
  activity serializes as JSON `entries: null`, not `entries: []` (this
  exact shape is even called out as correct in Stage 8's own
  `docs/PROGRESS.md` scoping-test walkthrough). `LedgerScreen.tsx` was
  reading `.length` off that `null` and crashing. Fixed on the frontend
  (`entries: res.entries ?? []` right after the fetch) rather than
  touching the backend, since Stage 9c is scoped frontend-only and the
  response shape itself is Stage 8's documented, intentional behavior —
  the frontend just needed to handle it. Caught by logging into the
  dashboard as a second, activity-free owner and finding a blank page
  where a clean empty state should have rendered.
- **`GET /owner/ledger`'s response has no `from`/`to` fields** (unlike the
  other four endpoints), so the Ledger screen is the one screen that can't
  render an `ActiveRangeNote` — it still sends the same range as every
  other screen, it just can't display the server's own echo of it. Noted
  in README.md rather than worked around by re-deriving a range client-side
  (which would violate the "never disagree with the ledger" spirit applied
  to dates too).
- **Recharts pinned at the 2.x line** (`^2.13.3`, resolved to `2.15.4`)
  rather than the newer 3.x — 2.x is stable, well-documented, and matches
  this repo's general preference (seen in Stage 9a/9b) for conservative,
  proven dependency versions over chasing the newest major; npm did flag
  2.x as no-longer-actively-developed, noted here rather than silently
  picked.
- **Fleet/Drivers render as dense HTML tables, not cards** — a deliberate
  desktop-first choice per the stage brief; a card-per-vehicle layout is
  the commuter/driver apps' idiom, not this one's.

Verified: `npm install`, `npx tsc --noEmit`, and `npm run build` both clean
(the only build warning is Vite's generic "chunk larger than 500kB" advisory
from bundling Recharts, not an error — not worth code-splitting for an MVP
demo bundle). End-to-end verified against the live backend with Playwright
(`playwright-core` pointed at the machine's cached Chromium, same approach
as Stage 9b-ii, since a fresh `npx playwright install` wasn't available in
this environment): seeded, charged five fares across both seeded vehicles,
ran `/fuel/allocate`, funded a vehicle's fuel quota, and authorized +
confirmed a mock VIU pump session so every figure had something non-zero to
show; then, as the seeded owner, screenshotted the Overview stat cards, the
Revenue vs Fuel chart (zero-based Y axis, Rand tick labels, grouped
bars + dashed consumption line), the Fleet table, the Drivers table, and the
Ledger table (fare/fuel_allocation/fuel_authorization rows with the
owner/driver/platform split spelled out, oldest-first pagination); switched
the date-range preset (Today → Last 7 days) and confirmed every screen's
figures and echoed range updated together. **Verified owner-scoping
visually**: logged in as a second, wholly unrelated seeded owner
(`+27820099999` / `Owner2Pass!`) and confirmed Overview/Fleet/Ledger all show
clean zeros/empty states, never owner 1's revenue, vehicles, or ledger rows
— this is where the nil-slice bug above was actually caught and fixed.

SCOPE HONESTY (per CLAUDE.md and the stage brief, stated in
`apps/owner/README.md`): no cross-cutting backend changes were made or
needed. Live fleet/driver online status still reflects Stage 4's current
in-memory telemetry (right now, not a historical timeline) exactly as Stage
8 already documented; this dashboard adds no new backend capability, only a
new way to read the existing one.

Next: Stage 9d — cross-cutting polish / demo prep, or the MVP feature set
(stages 0–9c) may be considered functionally complete for a demo, at the
team's discretion.

---

## Backend cleanup — `/stops`, commuter transaction history, list `[]` serialization — DONE (2026-07-14)

Additive/corrective backend-only pass closing three gaps the Stage 9 frontend
arc surfaced along the way. No changes to `apps/driver`, `apps/commuter`, or
`apps/owner` in this pass — existing frontend workarounds are still in place
and still work; this just makes the backend correct at the source.

**Gap 1 — list endpoints must return `[]`, not `null`.** Found the root
cause: several repo-level list queries declared their result as `var x []T`
(a nil slice until the first `append`), which `encoding/json` serializes as
`null` for the empty case. Most handlers already re-wrapped these into a
`make([]T, 0, len(x))` response slice before serializing (safe regardless of
whether the repo returned nil), but two call sites serialized the repo
result directly: `analytics.Repo.Ledger`'s `entries` (the exact bug Stage
9c's owner-dashboard `?? []` guard was worked around) and
`analytics.Repo.dailySeries`'s `buckets` (feeds `revenue-vs-fuel`'s series,
though its handler already re-wrapped it — fixed at the source anyway for
consistency). Fixed **at the source, everywhere**, by changing every
`var x []T` list-accumulator to `x := []T{}` before the scan loop, so a
nil-vs-empty distinction can never leak into a JSON response from any layer
again — not just the handlers that happened to re-wrap:
`analytics.Repo.{Ledger,dailySeries}`, `routing.Repo.{ListStops,ListRoutes,
ListLegsForRoute}`, `identity.Repo.{ListVehiclesByOwnerUserID,
ListDriversByOwnerUserID}`, `telemetry.VehicleStateStore.ListByRoute`. Picked
this over normalizing at the response boundary (e.g. a generic "nilToEmpty"
JSON-marshal wrapper) since it's one keystroke per call site, requires no new
abstraction, and matches the style every handler already uses
(`make([]T, 0, len(...))`) — one consistent rule: **lists are never nil past
the query loop that builds them.**
  - The Stage 9c owner-dashboard `?? []` guard in `apps/owner/src/screens/
    LedgerScreen.tsx` is now **redundant** — `GET /owner/ledger` returns a
    real `[]` for an empty range, so the guard is dead code, not a
    workaround for live behavior anymore. Left in place per this pass's
    frontend-untouched scope; safe to delete in a future frontend stage.
  - Tests: `internal/analytics/analytics_test.go`'s existing
    `TestEmptyState_NoActivityReturnsCleanZeros` had a latent gap of its
    own — its assertions used a bare `body["vehicles"].([]any)` type
    assertion and only checked length `if ok`, which silently reports
    `ok=false` (and skips the check entirely) for a JSON `null` exactly as
    readily as for "key absent" — so it could never have caught the `Ledger`
    nil-slice bug it was sitting next to. Replaced with a new
    `assertEmptyJSONArray` helper that explicitly fails on `null` before
    asserting the array is empty, and used it for `vehicles`/`drivers`/
    `entries`. New: `internal/routing/stops_handler_test.go`'s
    `TestListStops_EmptyRouteReturnsEmptyArray` (decodes the raw response
    body and asserts it's the literal bytes `[]`) and
    `internal/wallet/transactions_test.go`'s
    `TestTransactions_EmptyCommuterReturnsEmptyArray`.

**Gap 2 — `GET /stops` (server-side stop resolution).** Stage 9b-i's
commuter app had flagged this gap explicitly (`useRoutesData.ts`): with no
stops endpoint, it reconstructed the stop list client-side from `GET
/routes` + `GET /routes/{id}` per route. The repo-level `routing.Repo.
ListStops` already existed (used internally by `stopsByID` for other
handlers) — this just exposes it:
  - `GET /stops` — every stop (`id`, `name`, `latitude`, `longitude`),
    alphabetical, same ordering as `GET /routes`. Public, no auth — reference
    data, consistent with `/routes` (Stage 3) and `/telemetry/vehicles`
    (Stage 4) already being public.
  - `GET /stops?route_id=<id>` — added as the "your call, small addition"
    option, since a commuter's from/to picker actually needs a route's own
    stops **in physical sequence order**, not the alphabetical full list —
    exactly the ordering `boarding`'s `FareForSegment` (Stage 5) and the
    commuter app's Board screen (Stage 9b-ii) already assume. Derived from
    the route's ordered legs (`ListLegsForRoute`, Stage 3): the first leg's
    `from_stop`, then every leg's `to_stop`, walked in `sequence` order — no
    new query shape, reuses exactly what `GET /routes/{id}` already loads.
    404s on an unknown `route_id` (doesn't silently fall back to the
    unfiltered list); a route with zero legs returns `[]` (Gap 1).
  - `internal/routing/handlers.go` — new `ListStops` handler,
    `stopResponse`/`toStopResponse`. `internal/server/router.go` — wired
    `GET /stops` alongside the other public `/routes*` routes.
  - Tests (`internal/routing/stops_handler_test.go`, real Postgres, skips if
    unreachable like every other DB-backed test in the repo):
    `TestListStops_ReturnsSeededStops` (unfiltered list includes known
    seeded corridor stops by name), `TestListStops_EmptyRouteReturnsEmptyArray`,
    `TestListStops_FilteredByRoute_ReturnsOrderedStops` (against the
    synthetic A→B→I fixture already shared with `integration_test.go`,
    asserts exact sequence order, not alphabetical), `TestListStops_
    UnknownRoute404s`.
  - **Not done in this pass**: migrating `apps/commuter`'s `useRoutesData.ts`
    off its client-side reconstruction onto the new endpoint — that's a
    frontend change, out of scope here per the brief. The endpoint now
    exists for a future frontend stage to adopt.

**Gap 3 — `GET /wallet/transactions` (commuter transaction history).** The
commuter app could show a wallet balance (Stage 2's `GET /wallet/balance`)
but never its history — Stage 9b-ii's `WalletScreen.tsx` explicitly flagged
this as a client-side-session-log-only workaround, since no endpoint existed.
Added, following Stage 8's owner-ledger reconciliation and pagination
pattern exactly:
  - `GET /wallet/transactions?limit=&offset=` (auth: commuter only) — the
    caller's own `commuter_wallet` account postings, joined back to their
    parent `ledger_transactions` row, **newest first**
    (`ORDER BY lt.created_at DESC, lt.id DESC`), each with `transaction_id`,
    `kind` (`topup`/`fare`), `amount_cents` (signed, same convention as
    every other ledger figure in this codebase), `occurred_at`, and — for
    fare transactions only — `vehicle_id`/`vehicle_registration` (resolved
    via `identity.Repo.GetVehicleByID` from the `vehicle_id` Stage 2's
    `ChargeFare` already stores in `ledger_transactions.metadata`).
    `limit`/`offset` default 50/0, capped at 200, matching `/owner/ledger`'s
    exact pagination shape and defaults. Response:
    `{transactions, total, limit, offset}`.
  - **Route/trip context is NOT included** — flagged rather than silently
    dropped. A fare transaction's metadata carries `vehicle_id` (set once at
    charge time), not `route_id`; the driver-vehicle-route association that
    *would* answer "which route was this" lives only in Stage 4's in-memory
    `VehicleStateStore` (resets on restart, no historical log — the same
    accepted MVP trade-off Stage 8's package doc already documents for live
    fleet status). So a historical fare genuinely has no persisted route to
    report days later; `vehicle_registration` is the honest context that
    *is* available and durable.
  - **Strict scoping, same structural pattern as Stage 8's `/owner/*`**:
    identity comes from `identity.ClaimsFromContext` (the validated JWT),
    never a request parameter — there is no `commuter_id` field on this
    endpoint to even attempt passing. The query is `WHERE lp.account_id =
    $1` against the caller's own lazily-resolved `commuter_wallet` account
    id (`GetOrCreateAccount`, same lazy-creation Stage 2's `Balance` handler
    already uses), so a second commuter's rows are structurally unreachable,
    not merely filtered out after the fact.
  - `internal/wallet/models.go` — new `Transaction` (repo-level read model:
    `TransactionID`, `Kind`, `AmountCents`, `OccurredAt`, `VehicleID`).
    `internal/wallet/repo.go` — new `Repo.ListTransactionsForAccount`
    (postings→transaction join + a separate `COUNT(*)` for `total`, same
    two-query shape as `analytics.Repo.Ledger`). `internal/wallet/
    handlers.go` — new `Handlers.Transactions`; `Handlers`/`NewHandlers`
    gained an `identityRepo *identity.Repo` parameter (wallet already
    imported `identity` for `ClaimsFromContext`/`Role`, so no import cycle)
    — both call sites (`cmd/server/main.go`, `internal/server/health_test.go`)
    updated to pass it. `internal/server/router.go` — wired `GET
    /wallet/transactions` into the existing commuter-only route group
    alongside `POST /wallet/topup`.
  - Tests (`internal/wallet/transactions_test.go`, real Postgres, driven as
    raw HTTP through `identity.RequireAuth`/`RequireRole` with real bearer
    tokens, same "test it as a raw token over HTTP" approach Stage 5's
    boarding tests established): `TestTransactions_
    TopupsAndFaresOrderedAndReconciled` (a top-up + a fare charge appear
    newest-first, amounts sum to exactly the reported wallet balance — no
    separate tally, straight off postings — and the fare entry carries
    vehicle context while the topup doesn't), `TestTransactions_
    EmptyCommuterReturnsEmptyArray`, `TestTransactions_
    CrossCommuterIsolation` (commuter B, with their own zero activity, sees
    `[]` — never commuter A's top-up — mirroring Stage 8's
    `TestScoping_OwnerCannotSeeAnotherOwnersData` exactly).
  - **Not done in this pass**: migrating `apps/commuter`'s `WalletScreen.tsx`
    off its client-side session-log onto this endpoint — frontend change,
    out of scope here. The endpoint now exists for a future frontend stage
    to adopt.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` diff — no new dependencies). Full test suite
(`go test ./...`) passes against a live Postgres, including every new test
above and zero regressions in any prior stage's tests. `go test -race ./...`
(MSYS2 `ucrt64` gcc toolchain, same as Stages 4/5) also passes cleanly across
every package with **no data races detected**. End-to-end verified by hand
against the live seeded backend: `GET /stops` (full alphabetical list),
`GET /stops?route_id=<Cape Town CBD - Bellville>` (returned exactly
Cape Town Station → Parow → Bellville Station, in that physical order, not
alphabetical), `GET /wallet/transactions` as seeded commuter 1 (fare charges
newest-first with correct vehicle/registration context) and as seeded
commuter 2 (a disjoint set of their own fares tied to their own vehicle —
confirmed no overlap with commuter 1's transaction ids), and `GET
/owner/ledger` as a freshly-registered owner with zero activity — confirmed
the raw response body is the literal bytes `{"entries":[],...}`, not
`{"entries":null,...}`.

---

## Housekeeping — test-data isolation + cleanup — DONE (2026-07-14)

No feature changes. The dev Postgres had accumulated hundreds of junk
routes/stops from DB-backed integration tests that create fixtures against
the **persistent** dev database (not a disposable one) and, in three of the
four test files that do this, never cleaned them up. This cluttered `GET
/routes`, the new `GET /stops` (previous entry), and `cmd/seed`'s SEEDED
DATA summary. Two parts: stop the leak at the root, then clean up what had
already accumulated.

**Root cause, confirmed by querying the live dev DB (not guessed)**: a
`GROUP BY name` over `routes` showed exactly four junk-producing patterns —
`Boarding Test Route %` (96 rows), `Stops Test Route %` (54), `Telemetry
Other Route %` (12), `Telemetry Test Route %` (12) — and the matching stop
patterns `Boarding Test Origin/Dest %`, `Stops Test Origin/Mid/Dest %`,
`Telemetry Test Origin/Dest %` (378 stops total). Tracing each pattern to
its source:
  - `internal/routing/integration_test.go`'s `seedTestRoutes` **already**
    cleans up via `t.Cleanup` (Stage 3's original test-hygiene pass) — 0
    leftover rows from this file, confirming the pattern works when applied.
  - `internal/boarding/boarding_test.go`'s `seedFixture`,
    `internal/stops/integration_test.go`'s `seedRoute`, and
    `internal/telemetry/integration_test.go`'s `seedDriverOnRoute` (plus one
    inline `CreateRoute` call in `TestDriverUpdatePropagatesToCommuterOnSameRoute`
    for a second "other route") created stops/routes/legs with the same
    unique-per-call-timestamp naming discipline as every other DB-backed
    test in this repo, but **never removed them** — these three files are
    where all 174 junk routes / 378 junk stops came from.

**Fix chosen: (a) `t.Cleanup` in each fixture helper, matching the pattern
`routing/integration_test.go` already established** — not (b) a separate
"removable" naming convention (redundant; the names were already
unique/removable, just never removed) and not (c) transaction-rollback
isolation (would require restructuring every DB-backed test in the repo to
run inside one enclosing transaction shared with the handler-under-test's
own pool — a much bigger change than this housekeeping pass warrants, and
several of these tests exercise real HTTP/WebSocket servers backed by the
same pool, where a single outer transaction doesn't compose cleanly with
concurrent request handling). `t.Cleanup` deleting exactly the rows a test
created (by id, not by re-matching a name pattern) is the smallest change
that fixes the leak without touching what any test asserts:
  - `internal/boarding/boarding_test.go` — `seedFixture` now deletes its
    route's legs, the route, and its two stops in `t.Cleanup` (reusing the
    existing `env.pool` field already on `testEnv`).
  - `internal/stops/integration_test.go` — `testEnv` gained a `pool` field
    (previously private to `setup`, not stored); `seedRoute` now deletes its
    route's legs, the route, and its three stops (origin/mid/dest) in
    `t.Cleanup`.
  - `internal/telemetry/integration_test.go` — `setupTelemetryTest` now
    also returns the pool (signature grew from 4 to 5 return values; both
    call sites updated); `seedDriverOnRoute` now deletes its route's leg,
    the route, and its two stops in `t.Cleanup`;
    `TestDriverUpdatePropagatesToCommuterOnSameRoute`'s extra "other route"
    (created inline, not through `seedDriverOnRoute`) gets its own
    `t.Cleanup` too.
  - Deliberately **not** cleaning up the users/vehicles/drivers/commuters
    these same fixtures create — out of scope for this pass, which is about
    the routes/stops clutter specifically named in the brief; those rows
    don't show up in `GET /routes`, `GET /stops`, or the seed summary the
    way junk routes/stops do.
  - No test assertions changed anywhere — only when the rows they created
    get deleted.

**Cleanup tool for already-accumulated junk: `backend/cmd/cleanup`.** A
small, safe, idempotent Go command rather than a one-off SQL script, so it's
re-runnable via `go run` like `cmd/seed`/`cmd/cleanup` and shares
`internal/config`'s `DATABASE_URL` handling instead of hardcoding a DSN:
  - Matches routes by the exact four `LIKE` patterns identified above (see
    `routeNamePatterns` in `cmd/cleanup/main.go`) — never a broad wildcard,
    never touched the real 8 Cape Town corridor routes.
  - A stop is deleted only if it **both** matches one of the corresponding
    junk name patterns **and** has zero `route_legs` references left once
    the junk routes' legs are removed (`NOT EXISTS` against `route_legs`,
    re-checked inside the same transaction right before the stop `DELETE` —
    not reused from an earlier read) — so a stop can never be deleted while
    a real route still uses it, even under a hypothetical future name
    collision.
  - Deletion order respects the FK (`route_legs` before `routes`); nothing
    here touches `ledger_transactions`/`ledger_postings`, so Stage 2's
    zero-sum trigger is never in play — fare metadata stores `vehicle_id`,
    not route/stop ids, so routes/stops carry no ledger FK at all.
  - **Defaults to a dry run**: always prints the matched route count (with a
    sample, capped at 20 + "...and N more") and the stop count that would
    become orphaned, before doing anything. Pass `-apply` to actually
    delete; a bare run makes zero writes.
  - Wrapped in one transaction on `-apply`, so a failure partway through
    rolls back cleanly rather than leaving routes deleted but their legs
    not, or vice versa.

Verified end-to-end against the live dev DB: dry run reported exactly 174
matched routes / 378 orphanable stops (matching the `GROUP BY` counts found
during investigation); `-apply` deleted 216 `route_legs`, 174 routes, 378
stops in one transaction; a follow-up dry run reported **0/0** (idempotent —
nothing left to match); `cmd/seed`'s SEEDED DATA summary now lists only the
8 real corridors and their 12 real stops, with the same 3 interchanges as
Stage 3; `GET /routes` and `GET /stops` were spot-checked directly and show
only real data.

**Regression check — the fix actually stops the leak**: ran the full suite
twice in a row against the same (not reset) Postgres
(`go test -count=1 ./...`, then again), and a `go test -race -count=1 ./...`
pass on top of that — `SELECT count(*) FROM routes` / `stops` stayed at
exactly 8/12 after every run, and a diff against the real corridor name list
showed zero rows outside it after each run.

**Aside, found during this verification, NOT fixed here (out of scope,
flagged rather than silently left)**: `internal/fuel/fuel_test.go`'s
`TestAntiBypass_FuelFundsNeverReachWalletOrPayout` sums the **entire**
`commuter_wallet` account type across the whole database (not scoped to the
one owner/vehicle it creates) and asserts that total is unchanged across its
own run. Under `go test ./...`'s default cross-package parallelism, other
packages' tests (`wallet`, `boarding`, `stops`, `analytics`) are
concurrently topping up/charging real commuter wallets in the same shared
persistent DB, so this assertion can spuriously fail depending on scheduling
— reproduced once during this pass's verification, and confirmed to pass
reliably both alone (`go test ./internal/fuel/...`) and under `go test -p 1
./...` (packages run sequentially, no cross-package interference). This is a
pre-existing test-isolation gap unrelated to the routes/stops leak this
pass fixes (a global-aggregate assertion racing other packages, not
junk-data accumulation) — noted here for a future pass rather than fixed
under this housekeeping brief's scope. All verification above used `-p 1` to
route around it without weakening or skipping the assertion itself.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` diff). `go test -count=1 -p 1 ./...` and
`go test -race -count=1 -p 1 ./...` both pass in full, twice consecutively,
against the same live Postgres, with zero regressions in any prior stage's
assertions — only fixture data lifecycle changed, no test logic.

Cleanup / reseed commands (PowerShell):

```powershell
$env:DATABASE_URL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"

# Dry run — always do this first, shows exactly what would be deleted
go run ./cmd/cleanup

# Apply — actually deletes the matched junk routes/legs/stops
go run ./cmd/cleanup -apply

# Confirm cmd/seed's summary is clean again (only the 8 real corridors)
go run ./cmd/seed
```

---

## Fix the fuel anti-bypass flake — DONE (2026-07-14)

No feature changes; test-isolation only, following up on the housekeeping
pass above, which flagged but deliberately did not fix
`internal/fuel/fuel_test.go`'s `TestAntiBypass_FuelFundsNeverReachWalletOrPayout`
as a separate, pre-existing flake and worked around it with `-p 1` for that
pass's own verification. This entry fixes it properly so a bare `go test
./...`/`go test -race ./...` (no `-p 1`) pass reliably.

**Root cause**: the test read *global* aggregate state instead of its own
fixture's rows — `SUM(amount_cents)` across **every** `commuter_wallet` /
`driver_earnings` account in the database, the balance of the **one**
shared `funding_source` system account, and `COUNT(*)` over **all**
`ledger_postings` in the whole table. Under `go test ./...`'s default
cross-package parallelism, other packages (`wallet`, `boarding`, `stops`,
`analytics`) are concurrently topping up and charging real commuter/driver
wallets — and every `Topup` anywhere debits the same singleton
`funding_source` account by construction (Stage 2: at most one system
account per type) — against the same persistent dev Postgres, so any of
those global reads could change between this test's "before" and "after"
snapshots for reasons that have nothing to do with fuel logic.

**Fix — same guarantee, isolated data source, per the brief's own suggested
approach ("assert on the transaction/posting structure of its own isolated
operations rather than on a global balance snapshot")**:
  - `mustCreateFundedOwnerVehicle` (used by every other test in the file)
    is untouched. A new `mustCreateFundedOwnerVehicleFull` — same body, same
    unique-per-call identifiers every DB-backed test in this repo already
    uses — additionally returns the driver's and commuter's own user ids in
    a `fundedFixture` struct; the plain two-value helper is now a thin
    wrapper around it. Only the anti-bypass test calls the full form; the
    other 11 call sites in the file are unchanged.
  - `env.postingCount()` (global `COUNT(*) FROM ledger_postings`) replaced
    by `env.scopedPostingCount(ownerUserIDs...)` —
    `COUNT(*) ... WHERE a.owner_user_id = ANY($1)`. Called with exactly this
    test's own owner/driver/commuter user ids. This is safe under
    concurrency for two reasons: the shared system accounts
    (`funding_source`, `platform_fee`) have `owner_user_id IS NULL` and can
    never match an `ANY()` filter over non-null ids, and no other test in
    the suite knows these single-test-unique ids, so only this test's own
    calls can move the count.
  - `totalAcrossAccountType()` (global `SUM` per account *type*) removed —
    the commuter_wallet/driver_earnings before/after checks now call the
    existing `env.accountBalance(t, &fx.CommuterID, ...)` /
    `env.accountBalance(t, &fx.DriverUserID, ...)` helper (already used
    elsewhere in this file for the owner's own accounts), which was already
    scoped by `owner_user_id` — it just hadn't been pointed at this
    fixture's driver/commuter ids before.
  - The `funding_source`-balance before/after check (the one truly
    un-scopable read — there is exactly one such account, shared by the
    whole test binary) is replaced with a **structural** check on the one
    ledger transaction this flow is allowed to create: new
    `assertTransactionOnlyTouchesAccountTypes(t, env, allocateTxn.ID,
    wallet.AccountOwnerRevenue, wallet.AccountFuelAccount)` queries exactly
    that transaction's own posting rows (scoped by `transaction_id`, not a
    balance) and asserts the set of account types touched is exactly
    `{owner_revenue, fuel_account}` — which is strictly stronger than the
    old check (it rules out `funding_source` *and* `platform_fee` *and*
    `commuter_wallet` *and* `driver_earnings` in one assertion, not just
    `funding_source`), and is completely immune to concurrent writes
    anywhere else in the database since it only ever reads rows tied to
    `allocateTxn.ID`, an id only this test's own `Allocate` call produced.
  - Net result: every read in this test is now keyed by an id (`account.
    owner_user_id` or `ledger_transaction.id`) that only this test's own
    fixture/operations could have produced — the guarantee proved
    ("`FundVehicleQuota`/`AuthorizePump`/`ConfirmPump` add zero postings;
    `Allocate` touches only `owner_revenue`/`fuel_account`; this fixture's
    own `commuter_wallet`/`driver_earnings` never move") is identical to
    before, only which rows get read changed.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go mod tidy` all
clean (no `go.mod`/`go.sum` diff). `go test -count=1 -v
./internal/fuel/...` passes in full, including the rewritten anti-bypass
test alone and alongside the rest of the package's tests. Acceptance bar
from the brief — **no `-p 1`, run twice each**:
`go test -count=1 ./...` (green both times) and `go test -race -count=1
./...` (green both times, MSYS2 `ucrt64` gcc toolchain, same as prior race
runs) — all four runs passed cleanly against the same live, not-reset
Postgres, with zero flakes. Route/stop counts stayed at exactly 8/12
throughout (no regression on the housekeeping pass above).

---

## Baseline verified — clean-slate build green (2026-07-14)

A from-scratch verification pass, not new work: tore down the dev Postgres
entirely (`docker compose down -v` — drops the named volume, so this is a
genuinely empty database, not just an empty schema) and rebuilt everything
from nothing to confirm the repo's current state is reproducible, not an
artifact of an accumulated dev environment.

**Current state**:
- **Backend** (`sesfikile/backend`, Go): feature-complete through Stage 8,
  plus the two follow-up passes above — Stage 0 scaffold/infra, Stage 1
  identity, Stage 2 wallet+ledger, Stage 3 routing, Stage 4 telemetry,
  Stage 5 boarding (QR scan), Stage 6 request-a-stop, Stage 7 fuel (mock),
  Stage 8 owner analytics, the backend-cleanup pass (`/stops`, commuter
  transaction history, list `[]` serialization), the housekeeping pass
  (test-data isolation + `cmd/cleanup`), and the fuel anti-bypass
  test-isolation fix. Stage 9d (cross-cutting polish) is not started; the
  MVP feature set is otherwise complete.
- **Frontend**: three independent Vite/React/TypeScript apps, all built
  against the Stage 0-8 API surface only (no backend changes since) —
  `apps/driver` (Stage 9a, port 5174), `apps/commuter` (Stage 9b-i live
  map/search + 9b-ii wallet/boarding-pass/active-trip, port 5175),
  `apps/owner` (Stage 9c dashboard, port 5176).
- **Test suite**: race-clean and parallel-safe — `go test ./...` and
  `go test -race ./...` both pass with no `-p 1` needed (the fuel
  anti-bypass fix above was the last source of cross-package flakiness).
- **Seed data**: `cmd/seed` from an empty database produces exactly the 8
  real Cape Town corridor routes and 12 real stops (3 interchanges:
  Athlone, Cape Town Station, Wynberg) — no leftover test-generated junk,
  confirmed via `cmd/cleanup`'s dry run reporting 0/0 immediately after
  seeding.

**This verification pass**:
1. `docker compose down -v` then `docker compose up -d` in `infra/` — a
   genuinely empty Postgres 16, not a reset schema on an old volume.
2. `go run ./cmd/server` (or `cmd/seed` directly) applies all migrations
   from scratch against the empty database.
3. `go run ./cmd/seed` from empty — produced the 8 corridors / 12 stops
   above with no prior state to inherit.
4. `go test -race -count=1 ./...` — green, no `-p 1`.
5. In each of `apps/driver`, `apps/commuter`, `apps/owner`: `npm install`,
   `npx tsc --noEmit`, `npm run build` — all clean.

No code changes in this pass — this entry only records that the above was
run and passed against a genuinely empty environment.

### Standing this up from scratch (PowerShell)

```powershell
# 1. Fresh Postgres (drops any existing volume — genuinely empty)
cd infra
docker compose down -v
docker compose up -d
cd ..

# 2. Backend: migrate + seed from empty
cd backend
$env:DATABASE_URL = "postgres://sesfikile:sesfikile_dev@localhost:5432/sesfikile?sslmode=disable"
go run ./cmd/seed        # applies migrations, then seeds the 8 real corridors

# 3. Backend test suite (race-clean, parallel-safe — no -p 1 needed)
go test -race -count=1 ./...

# 4. Backend server (separate terminal, for frontend dev against it)
go run ./cmd/server

# 5. Each frontend app (separate terminals; ports 5174/5175/5176)
cd ..\apps\driver;   npm install; npx tsc --noEmit; npm run build; npm run dev
cd ..\apps\commuter; npm install; npx tsc --noEmit; npm run build; npm run dev
cd ..\apps\owner;    npm install; npx tsc --noEmit; npm run build; npm run dev
```

---

## Real route catalogue import (opt-in) — DONE (2026-07-14)

Backend-only, additive, **opt-in** — nothing here runs unless `cmd/importcatalogue`
is invoked by hand. `cmd/seed`'s hand-seeded 8-corridor/12-stop baseline is
byte-for-byte unchanged and is still exactly what a fresh `go run ./cmd/seed`
produces; every catalogue-imported row is tagged `source='catalogue'` (routes)
or has `latitude/longitude = NULL` (stops), both structurally distinguishable
from — and independently removable from — the seeded baseline. No frontend
apps touched.

**Input**: `backend/data/taxi_routes.csv` — a real City of Cape Town open-data
export, `OBJECTID, ORGN, DSTN, SHAPE_Length` columns, 1466 data rows.

### What's REAL, ESTIMATED, and MISSING (SCOPE HONESTY)

- **REAL**: origin rank name, destination rank name, route distance in
  metres. Directional pairs (A→B and B→A) and "VIA `<x>`" variants are kept
  as genuinely distinct routes, never deduplicated — including ~223
  origin/destination pairs that appear more than once in the source data
  with a *different* distance each time (up to 13× for one pair); every one
  of these becomes its own route, traceable back to its exact source row via
  the source CSV's own `OBJECTID`, embedded directly in the route name
  (`"<ORIGIN> - <DESTINATION> (CoCT #<OBJECTID>)"`) — this is also what makes
  re-running the importer idempotent.
- **ESTIMATED**: every catalogue route's fare. The source CSV carries **no
  fare data whatsoever** — `internal/catalogue.EstimateFareCents` derives an
  indicative fare from distance (`base + per-km rate`, rounded, clamped to a
  configurable `[min, max]`; defaults R5.00 base + R1.50/km, R6.00–R60.00 —
  all overridable via `CATALOGUE_FARE_{BASE,PER_KM,MIN,MAX}_CENTS` env vars,
  `internal/config.CatalogueFareModel`). Every such leg is flagged
  `fare_estimated: true` in `route_legs`/the API response, and both
  `cmd/importcatalogue`'s own printed output and this entry state plainly:
  **this is not an actual association tariff — real fares require
  association tariff data, which does not exist as an input to this MVP.**
- **MISSING**:
  - **Coordinates.** The source CSV has no lat/lng anywhere. Every
    catalogue-imported stop is created with `latitude`/`longitude = NULL`
    (`routing.Repo.CreateStopNoCoordinates`; `stops.latitude`/`longitude`
    are now nullable, paired by a `CHECK` constraint) and reports
    `Stop.CoordinatesKnown() == false`. Consequence, enforced in code, not
    just documented: catalogue stops/routes **cannot** appear on the live
    Leaflet map or in telemetry.
    - `GET /stops` (no `route_id` — the *map-facing* read) now calls the new
      `routing.Repo.ListStopsWithCoordinates`, which excludes every
      coordinate-less stop, so nothing consuming the unfiltered list for a
      map ever tries to place a marker at an unknown/zero position. Verified
      end-to-end: with all 1447 catalogue routes/549 catalogue stops loaded,
      `GET /stops` still returns exactly the 12 real seeded stops.
      `GET /stops?route_id=<id>` (the route-scoped browse/search read) is
      unaffected and still returns a catalogue route's own two stops —
      picking a from/to pair on a named route never needs a position.
    - `stops.Handlers.loadRouteStops` (Stage 6's request-a-stop matching,
      the other "telemetry read path" the brief called out) now checks
      `CoordinatesKnown()` for every stop on the requested route and returns
      a new `stops.ErrCoordinatesUnknown` if any lack one; `RequestStop`
      turns this into a clean `422` ("this route has no known stop
      coordinates … and cannot be used for live stop requests") rather than
      silently computing haversine distance against a nil position. Covered
      by `internal/stops/catalogue_test.go`'s
      `TestRequestStop_CoordinatelessRouteRejected`.
  - **Intermediate stops.** Only endpoints are known per source row, so every
    catalogue route is exactly **one leg** (`sequence = 1`,
    `routing.Repo.CreateCatalogueRouteLeg`) — no in-between stops to infer.
  - **Association sign-off.** Every catalogue route's `association_name` is
    the literal string `"City of Cape Town open data (unverified, no
    association attribution)"` (`internal/catalogue.CatalogueAssociationName`)
    — visibly distinct from the seeded corridors' real "Cape Town Minibus
    Taxi Association" label, so nobody mistakes one for the other.

### Built

- **Schema** (`backend/migrations/000005_route_catalogue.{up,down}.sql`),
  purely additive, every new column defaulting to what the existing
  hand-seeded rows already are:
  - `stops.latitude`/`longitude` — `NOT NULL` dropped, plus a
    `stops_coordinates_paired CHECK ((latitude IS NULL) = (longitude IS
    NULL))` so a stop can never end up with only half a coordinate.
  - `routes.source TEXT NOT NULL DEFAULT 'seed' CHECK (source IN ('seed',
    'catalogue'))`, indexed.
  - `route_legs.distance_meters DOUBLE PRECISION` (nullable — the source
    CSV's own `SHAPE_Length`, kept for traceability) and
    `route_legs.fare_estimated BOOLEAN NOT NULL DEFAULT false`.
- `internal/routing`:
  - `Stop.Latitude`/`Longitude` are now `*float64`; new
    `Stop.CoordinatesKnown() bool`. `Route` gained `Source string`
    (`SourceSeed`/`SourceCatalogue` constants); `RouteLeg` gained
    `DistanceMeters *float64`/`FareEstimated bool`.
  - New `Repo` methods: `CreateStopNoCoordinates`, `ListStopsWithCoordinates`
    (the map-facing read), `CountStopsWithoutCoordinates`,
    `CreateCatalogueRoute`, `CreateCatalogueRouteLeg`, `CountRoutesBySource`,
    and `DeleteCatalogueData` (one transaction: catalogue routes' legs →
    catalogue routes → any now-orphaned coordinate-less stop — never matches
    a `source='seed'` row or a stop with known coordinates, by construction).
    Every existing `CreateStop`/`CreateRoute`/`CreateRouteLeg` call site
    (`cmd/seed`, all prior tests) is unaffected — same signatures, same
    defaults.
  - `handlers.go`: `routeResponse` gained `source`; `legResponse` gained
    `fare_estimated`/`distance_meters` (omitted if unset); `stopResponse`
    gained `latitude`/`longitude` as nullable + `coordinates_known` — all
    additive JSON fields, no existing field renamed or removed.
- `internal/stops`: new `ErrCoordinatesUnknown`; `loadRouteStops` /
  `RequestStop` updated as described above.
- **`internal/catalogue`** (new package) — the importer itself:
  - `csv.go` — `ParseCSV`: BOM-tolerant, relies on `encoding/csv` for RFC
    4180 quoted-comma handling (no hand-rolled splitting), drops
    blank-ORGN/DSTN rows (counted, not silently discarded), keeps a handful
    of genuine same-origin/destination rows (rank-internal loop routes —
    real, not a parsing error).
  - `normalize.go` — `Normalize` + the small, hand-reviewed
    `variantCanonical` map (see "Conservative normalization" below).
  - `fare.go` — `EstimateFareCents` (see "ESTIMATED" above).
  - `import.go` — `Import`: idempotent load into `routing.Repo`, tagged
    `SourceCatalogue`; `routeName` embeds the source `OBJECTID`.
  - `clear.go` — `Clear`: thin wrapper over `Repo.DeleteCatalogueData`.
- `cmd/importcatalogue` — `go run ./cmd/importcatalogue [-csv path]`
  (default `data/taxi_routes.csv`, i.e. run from `backend/`). Applies
  migrations, imports, prints a real/estimated/missing summary plus row
  counts. Idempotent.
- `cmd/clearcatalogue` — mirrors `cmd/cleanup`'s shape exactly: dry-run by
  default (prints what would be removed), `-apply` to actually delete.
  Removes only `source='catalogue'` routes/legs and now-orphaned
  coordinate-less stops — structurally cannot touch the seeded baseline.

### Conservative normalization — the reviewable map

Built by grouping every unique raw rank name in the source CSV by a
punctuation/whitespace-stripped key (uppercase, strip every non-alphanumeric
character, **preserve word order**) — 24 groups came out with more than one
raw spelling. Every one was hand-reviewed and confirmed to be a TRUE
punctuation/spacing/apostrophe variant of the *same* physical rank, in the
*same* word order (e.g. `MITCHELL'S PLAIN` / `MITCHELLS PLAIN`,
`SANLAM CENTRE,PAROW` / `SANLAM CENTRE ,PAROW` / `SANLAM CENTRE PAROW`,
`HOUTBAY` / `HOUT BAY`) — never two genuinely different ranks, and never a
reordering of words (judged too big a leap for an auditable, mechanical
rule). `internal/catalogue/normalize.go`'s `variantCanonical` map folds
exactly those 24 groups and nothing else; its doc comment documents both the
folded groups and the deliberately-NOT-folded near-misses (the ten distinct
"KHAYELITSHA (…)" ranks; `"MITCHELLS PLAIN (TOWN CENTRE)"` vs `"TOWN CENTRE,
MITCHELLS PLAIN"` and `"PAROW SANLAM CENTRE"` vs `"SANLAM CENTRE, PAROW"` —
both reversed-word-order pairs, kept distinct). `internal/catalogue/
normalize_test.go`'s `TestNormalize_AgainstRealCSV` re-runs this exact
grouping live against the real CSV on every test run and fails if any
unfolded multi-spelling group appears that isn't this documented set — the
map can't silently drift out of sync with the source data.

### Row counts (a clean import from the 8/12 baseline)

```
Source rows read:        1466
Blank rows dropped:      19
Unique ranks (folded):   549    (576 raw spellings, 24 groups folded)
Routes imported (new):   1447
Stops created (new):     549
```

Verified against the live database after import: `routes` has exactly 8
`source='seed'` + 1447 `source='catalogue'` rows; `stops` has exactly 12
with real coordinates + 549 with `latitude IS NULL`. After
`cmd/clearcatalogue -apply`: back to exactly 8 routes / 12 stops, `cmd/
cleanup`'s dry run still reports 0/0 (unrelated test-junk sweep, unaffected).

### Tests

- `internal/catalogue/csv_test.go` — quoted-comma + BOM parsing, blank-row
  drop counted correctly, a same-origin/destination row kept (not dropped),
  malformed `OBJECTID`/short rows error cleanly.
- `internal/catalogue/normalize_test.go` — known variants fold to their
  documented canonical form; the brief's own conservative-folding
  non-negotiable (`KHAYELITSHA` / `KHAYELITSHA SITE C` / reversed-order pairs
  stay distinct); an unfamiliar name passes through untouched; the live
  audit against the real CSV described above.
- `internal/catalogue/fare_test.go` — base+per-km math, rounding to the
  nearest cent, min/max clamping (including the real dataset's actual
  shortest/longest distances), never negative.
- `internal/catalogue/import_test.go` (real Postgres, skips if unreachable
  like every other DB-backed test in this repo; cleans up only its own
  uniquely-suffixed rows via targeted `DELETE ... WHERE name LIKE`, **never**
  a blanket catalogue sweep, since this runs against the same shared dev
  database a developer might have a real import loaded in) — parses +
  tags `source='catalogue'` correctly (fare_estimated, distance_meters,
  coordinate-less stops all asserted), directional pairs and same-pair
  duplicate-distance rows create genuinely distinct routes, re-import is
  fully idempotent (second run: 0 new routes/stops), and a catalogue import
  never changes the `source='seed'` route count.
- `internal/routing/catalogue_repo_test.go` — `CreateStopNoCoordinates`
  round-trips with nil lat/lng, `ListStopsWithCoordinates` excludes a
  coordinate-less stop while including a real one (and the unfiltered
  `ListStops` still includes both), `CreateRoute` vs `CreateCatalogueRoute`
  tag `source` correctly, and `DeleteCatalogueData` removes a catalogue
  fixture while leaving a hand-seeded fixture completely untouched.
- `internal/stops/catalogue_test.go` —
  `TestRequestStop_CoordinatelessRouteRejected`: a route built from
  `CreateStopNoCoordinates`/`CreateCatalogueRoute` (exactly what the importer
  produces) is cleanly `422`'d by `POST /stops/request`, not silently
  matched against a zero position.

### Decisions / deviations

- **Route naming embeds the source `OBJECTID`** rather than a positional
  disambiguation counter — simpler, and every catalogue route stays
  individually traceable back to its exact source row, which a counter
  wouldn't give for free.
- **No cross-linking between catalogue and seeded stops**, even when a name
  would coincide (e.g. seed's `"Wynberg"` vs a hypothetical catalogue
  `"WYNBERG"`): catalogue names are stored exactly as normalized (upper
  case), seed names keep their existing Title Case, so the two never collide
  by construction. Deliberate — merging them would risk attaching real
  coordinates/telemetry expectations to a catalogue stop, or vice versa.
- **`stops.latitude`/`longitude` went nullable** (rather than a separate
  `coordinates_known` boolean) — one less column to keep in sync;
  `Stop.CoordinatesKnown()` is the single call site everything reads through.
- **Fare model lives in `internal/config`**, matching every other
  configurable-but-not-a-real-external-feed value in this codebase (fuel
  price/litre, fare split) — env-overridable, sane dev defaults, never
  pretending to be a real tariff.

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` diff — no new dependencies). Full suite
(`go test ./...`) and `go test -race -count=1 ./...` (MSYS2 `ucrt64` gcc
toolchain) both pass cleanly across every package, including every new test
above, with **zero regressions in any prior stage's tests** and the
untouched baseline (`go run ./cmd/seed` from a clean volume still produces
exactly 8 routes/12 stops). End-to-end verified by hand against a live
server: seeded the baseline, ran the real importer (1466 rows → 1447 routes,
549 stops, exactly as tabulated above), confirmed `GET /routes/search`
resolves a real catalogue route (`BELLVILLE → DURBANVILLE`, direct,
`fare_estimated: true`, real `distance_meters`), confirmed `GET /stops`
(unfiltered) still returns exactly the 12 real seeded stops with the full
catalogue loaded, confirmed `GET /routes/{id}` shows the catalogue route's
`source: "catalogue"` and distinct association label, then ran
`cmd/clearcatalogue -apply` and confirmed the database was back to exactly
8 routes / 12 stops with `cmd/cleanup` still reporting a clean 0/0.

### PowerShell — load, query, unload

```powershell
cd backend

# 1. Load the real catalogue (opt-in, additive, idempotent).
go run ./cmd/importcatalogue

# 2. Query real catalogue routes via the EXISTING /routes* endpoints — no
#    new endpoints were added. Run `go run ./cmd/server` in another terminal
#    first.
Invoke-RestMethod "http://localhost:8080/routes/search?from=BELLVILLE&to=DURBANVILLE"
Invoke-RestMethod "http://localhost:8080/routes/search?from=KHAYELITSHA&to=NYANGA"

# GET /routes still returns everything (8 seed + however many catalogue
# routes are loaded) — filter client-side on "source" if you just want one:
(Invoke-RestMethod "http://localhost:8080/routes") | Where-Object { $_.source -eq "catalogue" } | Select-Object -First 5

# Pull one real route's full detail (fare_estimated + distance_meters):
$bellville = (Invoke-RestMethod "http://localhost:8080/routes/search?from=BELLVILLE&to=DURBANVILLE").segments[0].route_id
Invoke-RestMethod "http://localhost:8080/routes/$bellville"

# GET /stops (unfiltered) stays exactly the 12 real seeded stops even with
# the full catalogue loaded — catalogue stops have no coordinates and are
# excluded from this map-facing read by design.
(Invoke-RestMethod "http://localhost:8080/stops").Count   # -> 12

# 3. Unload the catalogue back to the clean 8-corridor/12-stop baseline.
go run ./cmd/clearcatalogue          # dry run — shows what would be removed
go run ./cmd/clearcatalogue -apply   # actually removes it
```

**Superseded by the GeoJSON upgrade below** — the CSV importer (`ParseCSV`,
`-csv` flag) described above no longer exists; `GET /stops` unfiltered no
longer stays at 12 once the catalogue is loaded, since catalogue stops now
have real (approximate) coordinates. Kept this section for historical
context on the original opt-in design (source tagging, idempotency,
clear/undo) since all of that carried forward unchanged.

---

## Real route catalogue import: GeoJSON upgrade — DONE (2026-07-15)

Upgrades the opt-in importer above to read `backend/data/taxi_routes.json`
(the GeoJSON version of the same City of Cape Town dataset) instead of the
CSV — adding real geometry (polylines + endpoint coordinates) the CSV
lacked. Still backend-only, still additive, still opt-in: `cmd/seed`'s
8-route/12-stop baseline is untouched, and `go test -race ./...` is green
with the catalogue not loaded. No frontend apps touched (a follow-up will
render the polylines).

**Reused unchanged from the original entry**: the 24-entry conservative
name-normalization map and its live drift test, the blank-endpoint-row
drop, the distance-based `EstimateFareCents` model
(`config.CatalogueFareModel`) and `fare_estimated` labelling, and
`routes.source='catalogue'` tagging/idempotency (route names still embed the
source `OBJECTID`).

**Retired**: the CSV-only importer (`internal/catalogue/csv.go`, `ParseCSV`,
`cmd/importcatalogue -csv`). The GeoJSON is a strict superset of the CSV's
attributes (same `OBJECTID`/`ORGN`/`DSTN`, plus geometry), so maintaining two
parallel parsers had no upside — deleted rather than kept as a dead
alternate path, per this repo's usual "delete unused code" convention.
`backend/data/taxi_routes.csv` itself is left in place (tiny, harmless,
already committed) purely as a historical reference; nothing reads it
anymore.

**Migrating from an old CSV-loaded catalogue**: route/stop names are derived
identically from the same `ORGN`/`DSTN`/`OBJECTID` attributes in both files,
so a database that already has the CSV-era catalogue loaded would see every
row as "already imported" (same names) rather than getting enriched with
geometry. This is a full replacement, not an in-place upgrade — run
`cmd/clearcatalogue -apply` first, then re-import from the GeoJSON.

### A discovery that changed the plan: SHAPE_Length is in the wrong unit

Cross-checking the GeoJSON against the original CSV for the same feature
(`OBJECTID` 1, BELLVILLE→DURBANVILLE) showed `properties.SHAPE_Length` is
**0.1006** in the GeoJSON vs **12918.67** in the CSV — the GeoJSON's
`SHAPE_Length` turned out to be the path length in decimal degrees
(unprojected CRS84), not metres. Rather than depend on an attribute in the
wrong unit (or require having the CSV around just to cross-check it),
`internal/catalogue/geo.go`'s `polylineLengthMeters` computes each route's
real distance directly from its geometry — summing haversine distance
between every consecutive polyline vertex. This is arguably more correct
than trusting either file's own attribute: it's a direct, honest
measurement of the actual real-world path, not a value computed elsewhere
under an unstated (and in this case wrong) projection/unit assumption.
Sanity-checked against the CSV's own figure for `OBJECTID` 1 (10,714.8m
computed vs 12,918.67m in the CSV — same order of magnitude, consistent with
a great-circle sum vs a differently-projected polyline length, not a bug).

### NEW: rank coordinates from endpoints (APPROXIMATE)

For each feature, the polyline's **first point is the origin rank's
location** and the **last point is the destination rank's location**. A
rank appears as an endpoint across many routes with real spread (a large
area like Khayelitsha has routes departing from genuinely different points
~20km apart) — so `internal/catalogue/import.go`'s `prepareRows` does a
**first pass over the whole file**, before creating any stop, collecting
every endpoint sample a canonical rank name appears at (as an origin's first
point *or* a destination's last point, from any row), then
`geo.go`'s `medianCoordinate` computes that rank's stop coordinate as the
**median of its samples' longitudes and the median of its samples'
latitudes independently** — explicitly NOT the mean, which a handful of
outlier endpoints (or a single bad geometry row) can drag toward an
unrepresentative or even invalid location; the per-axis median snaps to
wherever the bulk of the samples actually cluster.

- This is the simpler **per-axis coordinate median**, not the true
  geometric median (which would minimise total distance to every sample via
  an iterative algorithm like Weiszfeld's) — sufficient for an approximate
  rank centroid and exactly what was asked for.
- Coordinates are keyed on the **same canonical (post-`Normalize`) rank
  names** the 24-entry folding map produces, computed in the same first pass
  that later creates stops — so every stop a normal import creates is
  guaranteed to have a coordinate; there is structurally no way for a
  rank's stop and its coordinate to key on different spellings.
- **Explicitly labelled approximate, not surveyed**: `routing.Stop`'s doc
  comment, `internal/catalogue/geo.go`'s `medianCoordinate` doc comment, and
  every printed/API-facing description of a catalogue stop all say so. A
  catalogue stop's `source` field (see below) is the one place a consumer
  can tell "this coordinate is a derived centroid" from "this coordinate is
  hand-placed and exact."

### NEW: route_geometries — polylines stored for later display

Each catalogue route's full polyline is stored via a new
`route_geometries` table (migration `000006`): `route_id` (PK/FK),
`geometry` (**JSONB**, a flat array of `[lon, lat]` pairs in path order),
`point_count`. **JSONB over PostGIS/a native geometry column**: every
feature in the source dataset is a `MultiLineString` with exactly one
`LineString` part (verified against the real file — 0 of 1466 features have
more than one part), so flattening to a plain point array loses nothing;
this MVP only ever needs to read a route's polyline back **whole**, for
display — no spatial queries (`ST_Intersects`, nearest-neighbour,
simplification) run against it. Reusing JSONB avoids a new Postgres
extension and a new Go dependency for a need this simple.
`routing.Repo.CreateRouteGeometry`/`GetRouteGeometry` marshal/unmarshal via
plain `encoding/json` — no new dependency. New public read:
**`GET /routes/{id}/geometry`** — `{route_id, point_count, points}`, 404 for
a route with none (every hand-seeded corridor, and correctly distinguishable
from "route doesn't exist" only in the message, not the status — neither
case has anything to draw).

If polyline storage ever bloats the database at scale, Douglas-Peucker
simplification is a **noted future option, not implemented here** — at
~394 points average × 1447 routes, current storage is unremarkable for a
JSONB column and didn't warrant the added complexity for this pass.

### A necessary fix uncovered along the way: stops needed their own `source`

Before this upgrade, a catalogue stop *always* had `latitude IS NULL`, so
`cmd/clearcatalogue`'s orphan-stop cleanup could safely find "every
catalogue stop" via that one check. Once catalogue stops get a real
(median-derived) coordinate, that check can no longer tell a catalogue stop
from a hand-seeded one — coordinate presence stopped being a reliable
provenance signal. Migration `000006` adds **`stops.source`**, mirroring
`routes.source` exactly (`DEFAULT 'seed' CHECK (source IN ('seed',
'catalogue'))`, indexed): `routing.Repo.CreateCatalogueStop` (new — real
coordinates, tagged catalogue) and `CreateStopNoCoordinates` (kept as a
defensive fallback, now also tagged catalogue) both set it explicitly;
`DeleteCatalogueData`'s stop-deletion query now scopes on `source =
'catalogue'` instead of `latitude IS NULL`. Caught by rewriting
`internal/routing/catalogue_repo_test.go`'s delete-scoping test to use
coordinate-*bearing* catalogue stops (the real post-upgrade shape) rather
than coordinate-less ones — the old version would have quietly stopped
proving anything once stops started getting real coordinates.

### Also uncovered: catalogue routes needed an explicit live-matching guard

Stage 6's request-a-stop matching (`internal/stops`) previously relied on a
coordinate check alone to keep catalogue routes out of live driver-matching
(no coordinates → automatic rejection). Now that catalogue stops have
coordinates, that guard would no longer by itself block one — a request
against a catalogue route would fall through to "zero online drivers on
this route" and return a harmless (if slightly misleading) "unmatched"
result rather than a clear rejection. `stops.Handlers.RequestStop` now checks
the route's `Source` explicitly and rejects a catalogue route with a `422`
("this is a catalogue-imported route with no live vehicles — stop requests
aren't available on it") **before** even loading its stops — an intentional,
self-documenting guard rather than an incidental consequence of "no driver
ever happens to be online there." The original coordinate-based guard
(`stops.ErrCoordinatesUnknown`) is kept too, as defense-in-depth for the
(now purely defensive) case of a genuinely coordinate-less stop on any
route.

### What's now map-capable vs. still missing

- **Map-capable (new)**: catalogue stops and routes now carry real
  (approximate, for stops) or real (exact, for polylines) geometry, and
  `GET /stops` (the map-facing read, `routing.Repo.ListStopsWithCoordinates`)
  now includes them alongside the 12 seeded stops — this is the intended
  upgrade, not an oversight. `GET /routes/{id}/geometry` exposes each
  catalogue route's real path for a future map to draw.
- **Still NOT map/telemetry-capable in the ways that matter**: no vehicle
  will ever go online on a catalogue route (no driver/vehicle data connects
  the two), and live stop-request matching is explicitly blocked by source
  (see above) — a catalogue route is real, browsable, and now drawable, but
  never live.
- **Still MISSING**: named intermediate stops (a polyline's ~394 vertices
  are shape points from the source geometry, not boarding stops — every
  catalogue route stays a single origin→destination leg, so multi-hop
  search via a named catalogue interchange still isn't possible). Fares
  remain ESTIMATED (see the original entry). Association sign-off is still
  absent.

### Provenance and the git/size decision

**Copyright: Western Cape Government, Department of Transport and Public
Works.** Dataset `SL_CGIS_TAXI_RTS`. Source API (reference only — never
fetched at runtime): `https://citymaps.capetown.gov.za/agsext/rest/services/
Theme_Based/ODP_SPLIT_6/FeatureServer/11` (serves EPSG:3857; the local file
is a one-time export already reprojected to WGS84, which is the entire
reason a local file is used instead of calling the API). All recorded in
`backend/data/README.md` (new) and in `internal/catalogue`'s package doc
comment.

**`backend/data/taxi_routes.json` (~16MB) is gitignored, not committed** —
added to the root `.gitignore`. Justification: it's large for a git repo
with no LFS/cloud setup, it's static reference data that never changes at
runtime (the importer only ever reads it), and it's trivially re-obtainable
from the source API (or wherever the original copy came from) — not worth
permanently bloating repository history with a 16MB blob. The original
~70KB CSV stays committed (already was, costs nothing to keep as a small
historical artifact). `backend/data/README.md` documents both files,
provenance, and how to obtain the GeoJSON; `cmd/importcatalogue` errors with
a pointer back to that README if the file is missing.

### Row/coordinate counts (a clean import from the 8/12 baseline)

```
Source rows read:        1466
Blank rows dropped:      19
Unique ranks (folded):   549
Routes imported (new):   1447
Stops created (new):     549
```

Verified against the live database after import: 549 catalogue stops, **0**
with `latitude IS NULL` (every rank got a median-derived coordinate — no
orphans in either direction); 1447 `route_geometries` rows (one per
catalogue route), averaging **395** points each (matches the source
dataset's ~394 pts/route). `GET /stops` (unfiltered) returned exactly
**561** stops (12 seed + 549 catalogue) with the full catalogue loaded —
up from 12 before this upgrade, the intended change. After
`cmd/clearcatalogue -apply`: back to exactly 8 routes / 12 stops / 0
`route_geometries` rows, `cmd/cleanup`'s dry run still 0/0.

### Tests

- `internal/catalogue/median_test.go` (new, pure — no database):
  `TestMedianCoordinate_ResistsOutliers` (a tight 5-point cluster plus one
  wild outlier — the median lands on the cluster; explicitly asserts the
  result is nowhere near the mean, which the outlier would have dragged
  far away), `TestMedianCoordinate_SinglePoint`, `TestMedianCoordinate_
  EvenCount`, and `TestPrepareRows_KnownRankGetsSaneCapeTownCoordinate` — a
  live check against the real `taxi_routes.json` (skips if absent) that
  `WYNBERG` and `CAPE TOWN STATION` both land within greater Cape Town's
  bounding box. All pure computation, so none of this can ever touch or
  risk a developer's real loaded catalogue.
- `internal/catalogue/geojson.go`'s parsing behaviour re-verified via
  `import_test.go`'s synthetic-fixture tests (rebuilt as small GeoJSON
  FeatureCollections, replacing the retired CSV fixtures): blank-row drop,
  directional/duplicate-distance distinctness, idempotency, and — new —
  `TestImport_EveryCreatedStopHasACoordinate` (no orphans in either
  direction) and `TestImport_MedianCoordinateResistsOutliers` (the same
  outlier-resistance property, exercised through a real `Import` call and
  the database, not just the pure function).
- `internal/catalogue/normalize_test.go`'s real-data audit renamed
  `TestNormalize_AgainstRealGeoJSON` and re-pointed at the GeoJSON — same
  logic, unchanged normalization map, now reading `taxi_routes.json`.
- `internal/routing/catalogue_repo_test.go`: new
  `TestCreateCatalogueStop_TaggedCatalogueWithRealCoordinates`; `ListStopsWithCoordinates`'s
  test renamed/extended (`TestListStopsWithCoordinates_MapFacingReadPath`) to
  prove a catalogue stop WITH a coordinate is now included (the upgrade)
  while a genuinely coordinate-less one still isn't; new
  `TestRouteGeometry_StoredAndRetrievable`; the delete-scoping test rebuilt
  around coordinate-bearing catalogue stops (`TestDeleteCatalogueData_
  OnlyRemovesCatalogueRoutesStopsAndGeometry`) to actually prove the new
  `source`-based scoping, not the coordinate-based one it silently used to.
- `internal/stops/catalogue_test.go`: new
  `TestRequestStop_CatalogueRouteWithRealCoordinatesStillRejected` proves the
  new explicit source-based guard specifically (a catalogue route whose
  stops DO have coordinates is still `422`'d); the original
  `TestRequestStop_CoordinatelessRouteRejected` kept as-is for the
  (now purely defensive) coordinate-based path.
- Baseline check: `TestImport_DoesNotAffectSeedBaseline` extended to assert
  both the seed-tagged **route** count and the seed-tagged **stop** count
  are unaffected by an import (stops needed their own baseline check once
  `stops.source` existed).

Verified: `go build ./...`, `go vet ./...`, `gofmt -l .`, and `go mod tidy`
all clean (no `go.mod`/`go.sum` diff — no new dependencies; geometry storage
uses plain `encoding/json` + JSONB, no PostGIS/spatial library). Full suite
(`go test ./...`) and `go test -race -count=1 ./...` (MSYS2 `ucrt64` gcc
toolchain) both pass cleanly across every package, including every new/
updated test above, with **zero regressions** and the untouched baseline
(`go run ./cmd/seed` from the clean state still produces exactly 8 routes/12
stops). End-to-end verified by hand against a live server with the REAL
16MB GeoJSON: import produced the exact table above; `GET /stops`
(unfiltered) returned 561 stops including a real catalogue stop with
`"source":"catalogue"` and a genuine coordinate (e.g. `"3RD /7TH AVE
MITCHELLS PLAIN"` at `-34.048953, 18.619164`); `GET /routes/search?from=
BELLVILLE&to=DURBANVILLE` resolved a real catalogue route with
`fare_estimated: true` and a haversine-computed `distance_meters`; `GET
/routes/{id}/geometry` returned that route's real 261-point polyline while
the same call against a hand-seeded corridor 404'd cleanly; `POST
/stops/request` against the catalogue route's own (coordinate-bearing) stop
was rejected `422` by the new source-based guard; and `cmd/clearcatalogue
-apply` restored the database to exactly 8 routes / 12 stops / 0
`route_geometries` rows, confirmed via direct query and via `cmd/cleanup`'s
dry run still reporting 0/0.

### PowerShell — load from GeoJSON, query (fare + coordinates + polyline), unload

```powershell
cd backend

# 1. Load the real catalogue from the GeoJSON (opt-in, additive, idempotent).
#    Requires backend/data/taxi_routes.json locally — see backend/data/README.md.
go run ./cmd/importcatalogue

# 2. Query a route showing its estimated fare, its stops' real coordinates,
#    and its retrievable polyline. Run `go run ./cmd/server` in another
#    terminal first.
$search = Invoke-RestMethod "http://localhost:8080/routes/search?from=BELLVILLE&to=DURBANVILLE"
$search.segments[0] | Select-Object route_name, fare_cents | Format-List
$search.segments[0].legs[0] | Select-Object fare_cents, fare_estimated, distance_meters | Format-List

$routeId = $search.segments[0].route_id
Invoke-RestMethod "http://localhost:8080/routes/$routeId"     # source: "catalogue"

# Confirm the route's endpoints now have real (approximate) coordinates —
# GET /stops is map-facing and now includes catalogue stops too:
$stops = Invoke-RestMethod "http://localhost:8080/stops"
$stops.Count                                                    # -> 561 (12 seed + 549 catalogue)
$stops | Where-Object { $_.name -eq "BELLVILLE" } | Format-List  # source: "catalogue", real lat/lng

# Fetch the route's real polyline:
$geometry = Invoke-RestMethod "http://localhost:8080/routes/$routeId/geometry"
$geometry.point_count
$geometry.points[0]     # first point
$geometry.points[-1]    # last point

# A hand-seeded corridor has no geometry — confirm the clean 404:
$seedRouteId = (Invoke-RestMethod "http://localhost:8080/routes" | Where-Object { $_.source -eq "seed" } | Select-Object -First 1).id
try { Invoke-RestMethod "http://localhost:8080/routes/$seedRouteId/geometry" } catch { $_.Exception.Response.StatusCode }

# 3. Unload the catalogue back to the clean 8-corridor/12-stop baseline.
go run ./cmd/clearcatalogue          # dry run — shows what would be removed
go run ./cmd/clearcatalogue -apply   # actually removes it, including route_geometries
```

Next: Stage 9d — cross-cutting polish, or a frontend follow-up to render catalogue polylines (as directed)

---

## Stage 9b-iii — commuter app: network coverage layer — DONE (2026-07-15)

Extends `apps/commuter` (9b-i map, 9b-ii wallet/pass) with a "network
coverage" map layer that draws the real 1447-route City of Cape Town
catalogue as a muted backdrop, plus catalogue-aware badging everywhere a
route or stop can appear. Frontend-only except for one new minimal backend
read endpoint (justified below). Core principle held throughout: catalogue
(browse-only, real geometry, estimated fares, no vehicles) must never be
visually confusable with live (real vehicles, tested fares) — see CLAUDE.md.

### The one backend addition: `GET /routes/geometries`

The brief allowed exactly one minimal bulk-read endpoint if per-route
fetches wouldn't scale, and they clearly wouldn't: 1447 routes via
`GET /routes/{id}/geometry` is 1447 round trips before a single pixel of
coverage could be drawn. Added:
- `internal/routing.Repo.ListRouteGeometries` — one query, `SELECT route_id,
  geometry FROM route_geometries ORDER BY route_id`. Only catalogue routes
  ever have a `route_geometries` row (only `internal/catalogue`'s importer
  ever calls `CreateRouteGeometry`), so this naturally excludes every
  hand-seeded corridor with no source filter needed.
- `internal/routing.Handlers.ListRouteGeometries` — `GET
  /routes/geometries[?max_points=N]`, wired into the existing public
  `/routes*` route group (no auth, consistent with `/routes`/`/stops`).
  Decimates each polyline server-side to `max_points` (default 40, `0` =
  no decimation): a simple even-stride selection that always keeps the
  first/last point (so a rank's real endpoint is never visually clipped),
  not Douglas-Peucker — sufficient for a muted backdrop that was never meant
  to be the map's focal content, noted as a future upgrade if closer-zoom
  fidelity is ever needed. Response: `[{route_id, original_point_count,
  points}]`.
- Measured against the real 1447-route catalogue: undecimated the dataset
  is 1447 routes × ~395 pts ≈ 571k points; decimated to 40/route it's
  57,680 points, a 1.4MB JSON response, served in ~250ms locally. One
  request, fetched lazily (only when the coverage toggle is first switched
  on, not on every app load), cached client-side for the session.

### Performance approach for drawing it: bulk fetch + one multi-polyline + canvas renderer

Rejected per-route `<Polyline>` React elements (1447 separate Leaflet Path
layers) as the obvious way to blow this up. Instead:
1. **Bulk fetch** (above) — one request, not 1447.
2. **Server-side decimation** (above) — 57.7k points instead of 571k.
3. **One `<Polyline positions={...}>` with a multi-line coordinate array**
   (`[number,number][][]`) instead of 1447 separate `<Polyline>` elements —
   Leaflet's `L.polyline` natively supports disjoint multi-line geometry as
   a single Path/Layer, so this is one Leaflet layer object for the entire
   network, not 1447 React-managed ones.
4. **`preferCanvas` on `MapContainer`** — routes all vector layers (this
   polyline included) through Leaflet's canvas renderer instead of SVG, so
   drawing/redrawing 57.7k points on pan/zoom is one canvas repaint, not
   thousands of DOM node updates.
- **Viewport-bounded rendering was considered and not needed** — the brief
  asked for the smoothest approach and to justify the choice; measured pan
  interaction (Playwright-driven drag, see Verified below) stayed under
  400ms with the full network drawn, so the added complexity of
  bounds-filtering client-side wasn't justified for this dataset size.
  Left as a documented future option if the catalogue ever grows
  materially larger.
- Coverage lines use Leaflet's default pane z-indices, unmodified —
  `Polyline` defaults to `overlayPane` (z-index 400), `Marker` to
  `markerPane` (z-index 600), so a live vehicle marker is *structurally*
  always drawn above the backdrop, not just by JSX ordering luck (which was
  also kept correct: markers render after the polyline in source order).

### Frontend changes

`apps/commuter/src/types.ts` — added `RouteSource` (`"seed" | "catalogue"`),
`Route.source`, `RouteLeg.fare_estimated`, `Stop.latitude/longitude/
coordinates_known/source`, and `RouteGeometrySummary`. `api/client.ts` —
added `getStops()` (`GET /stops`) and `getRouteGeometries(maxPoints?)`
(`GET /routes/geometries`).

**A necessary fix uncovered along the way: `useRoutesData` no longer
prefetches every route's detail.** The hook previously (9b-i, back when
there was no `GET /stops`) built its stop list by fetching *every* route's
detail up front — fine at 8 routes, a ~1447-request stampede once the
catalogue is loaded, which would have made this stage's own map
unreachable before a screenshot could even be taken. Rewrote it to: fetch
`GET /routes` (list only) and `GET /stops` (the endpoint a prior backend
pass — "Gap 2" in the housekeeping entry above — built for exactly this,
never previously adopted client-side) in parallel on load, and expose a new
`getRouteDetail(routeId)` that fetches-and-caches one route's detail lazily,
only when a screen actually needs it. `RoutesScreen` and `BoardScreen`
(the only two consumers of a route's ordered legs) now call this on
selection via a small new `useRouteDetail` hook instead of reading from a
pre-populated `Map`. `catalogueLoaded` (any stop tagged `source:
"catalogue"`) is derived from the already-fetched stop list — no extra
request — and is what every graceful-degradation check below reads.

**`RouteSourceBadge`** (new, `src/components/`) — the one visual marker
used everywhere a route's identity needs to be shown: a teal "LIVE" pill
(reusing the app's existing "transit" tone, already the "currently moving"
color) or a muted dashed-border "COVERAGE" pill. Used in `RoutesScreen`'s
list and detail, `SearchScreen`'s result header and per-segment rows.

**`MapScreen`** — the main deliverable:
- The "Watching route" picker now lists only live/seeded routes
  (`routes.filter(r => r.source !== "catalogue")`) — a catalogue route can
  never have a vehicle, so listing all 1447 of them there would be both
  useless and, at that size, a broken dropdown. This is also what keeps the
  existing three-state gating (pick a route / no vehicles / live) working
  unchanged when coverage is off — verified byte-for-byte visually
  identical to pre-stage behaviour (see Verified).
- New **network coverage toggle**, its own control independent of "watching
  a route" (coverage shows the whole network; watching shows one route's
  vehicles). Lazily fetches geometries via `useRouteGeometries` (new hook)
  only on first switch-on, cached after. **Degrades gracefully when the
  catalogue isn't loaded**: the checkbox is `disabled` (not hidden, not
  erroring) with an inline hint — "Not available — this backend has no
  route catalogue imported" — verified against a genuinely clean 8/12
  baseline.
- Coverage ON changes the map's visibility rule: previously the map only
  rendered when a route was selected and had vehicles; now the map surface
  itself *is* the coverage layer, so it stays up regardless of
  route-watching state. If a route happens to be selected with zero
  vehicles while coverage is on, that's now a small non-blocking overlay
  chip ("No vehicles online on X right now") instead of the old
  full-screen empty state, so the backdrop stays visible underneath it.
- **Legend**, shown only when coverage is on, using the brief's own wording
  verbatim: a small vehicle-marker swatch + "Live routes — vehicles running
  now", and a dashed-line swatch + "Network coverage — real City of Cape
  Town route data, no live vehicles, fares estimated".

**`SearchScreen`** — origin/destination `<select>`s now use `<optgroup>`
("Live ranks" / "Network coverage (browse only)") built from `Stop.source`,
so the picker's 561 stops (12 live + 549 catalogue, once loaded) stay
scannable without hiding either group — the brief's explicit "don't hide
the catalogue ranks, just label them truthfully." A search result whose
segment(s) resolve to a catalogue route (checked via `Route.source` looked
up by `route_id`, never guessed from the name) gets: a "COVERAGE" badge
next to the transfer/direct heading, "Est. total fare" instead of "Total
fare" in the total, an explicit browse-only/estimated-fare/no-boarding-pass
disclaimer paragraph, and a per-segment badge on each route button. A live
result is visually unchanged (still gets a "LIVE" badge on its segment
button, confirmed fully functional end-to-end in Verified).

**`BoardScreen`** — the "Route" picker in trip selection now filters to
`routes.filter(r => r.source !== "catalogue")` (with a one-line caption
explaining why fewer routes appear here than in the Routes tab). This is a
small, deliberate extension of the brief's "must not offer generate
boarding pass / request-a-stop as if rideable" principle: `BoardScreen`'s
own picker is the *only* path in this app that can issue a real boarding
pass (`SearchScreen` never offered one directly — only a "view route"
drill-down into `RoutesScreen`, which is read-only), and the backend's
`POST /boarding/pass` has no source check of its own (issuing a pass for a
catalogue route would succeed and produce a token that can structurally
never be scanned, since no vehicle ever exists on a catalogue route).
Omitting catalogue routes here entirely — rather than listing them
disabled with an asterisk — was judged the more honest reading of "never
visually confusable, never implies ride-able-later" for a picker whose
entire purpose is starting a real ride.

**`RoutesScreen`** — kept as the browse-everything list (all 1455 routes
once the catalogue is loaded, unchanged in scope from 9b-i); each row and
the detail view now carry a `RouteSourceBadge`, and a catalogue route's
detail view adds the same browse-only/estimated-fare disclaimer paragraph
used in Search, plus "Estimated full-route fare" instead of "Full-route
fare". Not restructured into a search/filtered view — out of this stage's
explicit scope (map, search, stop pickers) — but the badge means a user
scrolling it is never left guessing which of the 1455 entries are real
right now.

### Verified

`npx tsc --noEmit` and `npm run build` (apps/commuter) both clean. Backend:
`go build ./...`, `go vet ./...`, `gofmt -l .`, `go mod tidy` all clean (no
new dependency — `ListRouteGeometries` reuses `encoding/json` + the existing
JSONB column), and `go test -race -count=1 ./...` green across every
package with the catalogue unloaded (baseline untouched).

End-to-end, Playwright-driven (Chromium, same cached-browser approach as
prior frontend stages) against a live backend:
- **Catalogue NOT loaded (clean 8/12 baseline)**: map screen identical to
  pre-stage behaviour; coverage toggle present but disabled with the
  graceful-degradation hint; no errors.
- **Catalogue loaded** (1447 routes / 549 catalogue stops via
  `cmd/importcatalogue`, real GeoJSON): (a) coverage OFF — map screen
  unchanged from baseline, confirming the live-route-only picker keeps the
  old three-state behaviour intact even with 1447 extra routes in the
  system; (b) coverage ON with no route watched — the full network renders
  as thin muted dashed lines over blank/unloaded tiles (OSM fetch was
  offline in this sandboxed run — the app's own documented "one online
  dependency" limitation, not a bug) with the legend correctly captioned;
  brought a seeded driver online via `cmd/wsdriver` on "Bellville - Cape
  Town CBD", selected it in the picker — the live teal vehicle marker
  (seat count visible) renders clearly on top of the coverage backdrop, and
  a mouse-drag pan (~355ms) redraws both layers smoothly with no visible
  lag or artifacts; coverage fetch-to-rendered settle time measured at
  ~560ms for the full 57.7k-point network; (c) `GET /stops` grouped
  correctly into 12 "Live ranks" / 549 "Network coverage (browse only)"
  optgroups in both Search pickers; a catalogue search (BELLVILLE →
  DURBANVILLE) showed the COVERAGE badge, "Est. total fare", the
  browse-only disclaimer, and a badged segment button; (d) a live search
  (Cape Town Station → Khayelitsha Town Centre) rendered exactly as before
  9b-iii, with a LIVE badge on its segment button, fully functional;
  Board's route picker confirmed to list exactly 8 options (the seeded
  routes only, verified via option count) with 1447 catalogue routes
  correctly absent; Routes tab confirmed to badge every one of the 1447
  catalogue entries and show the browse-only disclaimer + estimated-fare
  label on a catalogue route's detail view. Zero browser console
  errors/exceptions across the whole run. Restored the clean 8-route/
  12-stop/0-geometry baseline afterward via `cmd/clearcatalogue -apply`,
  confirmed via direct DB query.

Next: Stage 9d — cross-cutting polish.
