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
