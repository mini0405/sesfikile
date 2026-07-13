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
