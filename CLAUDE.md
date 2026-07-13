# Ses'fikile — project context for Claude Code

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