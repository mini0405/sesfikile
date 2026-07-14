# Ses'fikile — driver app

A mobile-first React + TypeScript web app for minibus taxi drivers: go online on a
route, stream live GPS, scan commuter boarding passes to charge fares, manage
seats, check earnings, and respond to stop-request alerts.

## What it does

- **Login** — phone + password against `POST /auth/login`. Rejects non-driver
  accounts.
- **Home** — pick a route (`GET /routes`) and go online. Going online opens the
  bidirectional `GET /ws/driver` WebSocket (JWT passed as a `token` query
  param, since browsers can't set custom WebSocket handshake headers) and
  starts streaming `{lat, lng}` from `navigator.geolocation.watchPosition`
  over that socket. Reconnects with backoff if the connection drops while
  still "online".
- **Scan** — the hero action. Opens the device camera (via `html5-qrcode`) to
  scan a commuter's boarding-pass QR code, or accepts a pasted token as a
  fallback for desktop/dev testing. Either path calls `POST /boarding/scan`
  and shows a receipt (fare, driver/owner/platform split, seats remaining),
  clearly distinguishing a fresh charge from an idempotent replay (a pass
  scanned twice is never charged twice).
- **Seats & earnings** — adjusts `seats_available` via `POST /telemetry/seats`
  (±1 buttons) and shows the driver's `driver_earnings` wallet balance via
  `GET /wallet/balance`. There is no driver-specific "trips today" endpoint
  yet (that's currently owner-only, under `/owner/*`), so this screen shows
  balance only.
- **Alerts** — the same `/ws/driver` connection also receives server-pushed
  `stop_request` messages (Stage 6). They're listed here; acknowledging one
  calls `POST /stops/request/{id}/ack`.

## Design direction

The UI is deliberately not generic dark-mode SaaS. It's built around one real
artifact from this world: the hand-lettered destination board a minibus taxi
driver displays in the windscreen. That board is the app's signature
component (`.board` in `src/index.css`) — cream cardstock, marker-black
lettering, taped corners — reused for login, the route/status header, and the
earnings readout. The boarding-scan receipt is styled as a torn till-slip
ticket with a rotated rubber-stamp verdict (`PAID` in taxi-blue for a fresh
charge, `ALREADY PAID` in brake-red for a replay) — a direct visual answer to
the one thing that screen has to communicate. Status indicators throughout
read like dashboard instrument lights, not soft pill badges. The palette is
named after its source, not abstract brand tokens: `board` (destination-board
cream), `ink` (marker pen), `rank` (rank/curb-paint yellow — the primary
action color), `taxi` (livery blue), `brake` (brake-light red, reserved for
alerts/replays), `tar` (the asphalt backdrop, warm near-black rather than
cool slate). See `tailwind.config.js` for the full token set and
`src/index.css`'s `@layer components` for the board/ticket/stamp/led
component classes shared across screens.

## Backend endpoints used

| Screen | Endpoint |
| --- | --- |
| Login | `POST /auth/login` |
| Home | `GET /routes`, `GET /ws/driver?route_id=&token=` |
| Scan | `POST /boarding/scan` |
| Seats | `POST /telemetry/seats` |
| Earnings | `GET /wallet/balance` |
| Alerts | (pushed over `/ws/driver`), `POST /stops/request/{id}/ack` |

## Running it

Backend and Postgres must already be running (see the repo root `CLAUDE.md`
and `docs/PROGRESS.md`):

```powershell
# In backend/, in two separate terminals:
go run ./cmd/seed
go run ./cmd/server
```

Then, from `apps/driver/`:

```powershell
npm install
npm run dev
```

Open **http://localhost:5174**. Log in with a seeded driver, e.g. phone
`+27820000002`, password `Driver123!` (see `cmd/seed`'s printed output for
the current seeded logins).

`.env` already points at `VITE_API_BASE_URL=http://localhost:8080`; copy
`.env.example` to `.env.local` if you need to override it (e.g. a different
backend port).

## Secure-context constraint (read before testing on a phone)

The Geolocation and Camera (`getUserMedia`, used by the QR scanner) browser
APIs both require a **secure context**: `https://`, or the special-cased
`http://localhost`. This app works out of the box in dev on a desktop browser
at `http://localhost:5174`. It will **not** work if you open
`http://<your-lan-ip>:5174` on a phone on the same network — most mobile
browsers silently deny both permissions over plain HTTP on a non-localhost
host. On-device phone testing needs either a tunnel (e.g. a dev HTTPS proxy)
or a real TLS certificate, neither of which is set up yet — flagged here as a
known gap for a later stage rather than solved in this one.

## Notes

- **No state library** — React hooks + one `AuthContext` are enough for this
  app's size; nothing here justified adding Redux/Zustand/etc.
- **Token storage**: the JWT lives in memory and in `sessionStorage` (not
  `localStorage`), so it survives a page refresh but clears when the tab
  closes — a deliberate middle ground for a dev-only MVP with no
  refresh-token flow, not a hardened choice. See the comment in
  `src/context/AuthContext.tsx`.
- **Dev-only CORS**: the backend's `internal/server/router.go` gained a
  permissive, allow-all CORS middleware (`devCORS`) so this app (a different
  origin, `localhost:5174` vs. the API's `localhost:8080`) can call it from
  the browser. It's explicitly commented as dev-only in the backend code —
  no production/cloud deployment exists yet for this MVP.
