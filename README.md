# Ses'fikile

A closed-loop digital transit ecosystem for the South African minibus taxi
industry — cashless fares, live vehicle tracking, and automated fuel
disbursement in one platform. This repo is the local MVP.

## MVP scope
- Commuter app: map, wallet, route search, QR boarding pass
- Driver app: QR scan, GPS broadcast, earnings, fuel status
- Owner dashboard: fleet overview, analytics, driver admin, fuel config

## Stack
Go modular monolith · React + Vite + TypeScript · Postgres ·
Leaflet/OpenStreetMap · Docker Compose

## Running it locally

Prerequisites: Docker Desktop, Go 1.22+, Node.js. First time only, install
each frontend app's dependencies (`npm install` in `apps/driver`,
`apps/commuter`, `apps/owner`).

Then, from the repo root (PowerShell):

```powershell
.\scripts\dev-up.ps1
```

This starts Postgres, seeds dev data, and launches the backend plus all
three frontend dev servers, each in its own window, printing all four URLs
once ready. Add `-WithCatalogue` to also load the real ~1447-route City of
Cape Town route catalogue (opt-in, browse-only). When you're done:

```powershell
.\scripts\dev-down.ps1
```

For a full step-by-step manual test of the system — seeded logins, the core
ride loop, the idempotency demo, request-a-stop, known limitations, and
troubleshooting — see **[docs/TESTING.md](docs/TESTING.md)**.

See **[docs/PROGRESS.md](docs/PROGRESS.md)** for the full build log, stage
by stage, and **[CLAUDE.md](CLAUDE.md)** for project context/conventions.