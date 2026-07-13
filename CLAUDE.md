# Ses'fikile — project context for Claude Code

> Read `docs/PROGRESS.md` first to see what's already built. Append a new dated entry to it
> at the end of every stage.

## What this is
Ses'fikile is a closed-loop digital transit ecosystem for South African minibus
taxis: cashless fares, live tracking, automated fuel disbursement. This repo is
the local MVP.

## Architecture
A Go modular monolith exposing REST + WebSocket. Each module maps 1:1 to a future
microservice — keep boundaries clean so they can be split out later.
Modules: identity, wallet, routing, telemetry, fuel, analytics.

## Stack
- Backend: Go 1.22+, chi (REST), gorilla/websocket, pgx (Postgres), golang-migrate
- Frontends: React + Vite + TypeScript, Tailwind, Leaflet + OpenStreetMap, html5-qrcode
- Data: Postgres (ledger + registries). Redis optional.
- Local infra: Docker Compose (Postgres). No cloud.

## Non-negotiables
- The ledger is double-entry and ACID: debits must always equal credits; balances never go negative.
- Boarding QR codes are HMAC-signed and must be signature-verified on scan. Reject tampered/expired codes.
- Fare deduction is idempotent — a replayed scan must not double-charge.
- The eFuel / FuelOmat / VIU hardware is MOCKED in the MVP. Do not claim real hardware integration.

## Conventions
- Conventional commits: feat / fix / test / chore.
- Every module ships with tests. Heaviest coverage on wallet/ledger and boarding.
- One stage at a time; tests must pass before moving on.

## Build stages
0 scaffold+infra · 1 identity · 2 wallet+ledger · 3 routing · 4 telemetry ·
5 boarding (QR scan) · 6 request-a-stop · 7 fuel (mock) · 8 owner dashboard · 9 polish+offline

### Stage 0 — done
- Monorepo skeleton: `apps/{commuter,driver,owner}`, `packages/shared`, `docs` (placeholders only).
- `backend/` Go module (`sesfikile/backend`): `cmd/server`, `internal/{config,db,server}`.
- `GET /health` — 200 `{"status":"ok","db":"ok"}` when Postgres is reachable, 503
  `{"status":"degraded","db":"down"}` otherwise. Server always starts even if Postgres is down
  (pgxpool connects lazily; only `/health` actually pings).
- Graceful shutdown via `signal.NotifyContext` (SIGINT/SIGTERM).
- `infra/docker-compose.yml`: Postgres 16, container `sesfikile-postgres`, user/pass/db
  `sesfikile`/`sesfikile_dev`/`sesfikile`, port 5432, named volume, `pg_isready` healthcheck.
- Tests: `internal/config` (env defaults/overrides), `internal/server` (health handler, both
  branches via a fake pinger — no live DB needed). `go build ./...` and `go test ./...` pass.
- Known local env quirk: this dev machine also has a native Windows PostgreSQL 18 service
  bound to port 5432, which will shadow the Docker container on `localhost:5432` if both run
  at once — stop that service (or free the port) before using the Compose Postgres.