# Ses'fikile — commuter app (Stage 9b-i + 9b-ii)

Vite + React 18 + TypeScript (strict) + Tailwind. Dev server on port **5175**
(backend on 8080, driver app on 5174). `VITE_API_BASE_URL` in `.env`/`.env.example`
(default `http://localhost:8080`).

Stage 9b-i built the first half (sign-in, live vehicle map, route search,
route detail). **Stage 9b-ii adds the second half — wallet, boarding-pass
generation, and a light active-trip view — closing the loop with the driver
app (Stage 9a): a commuter tops up here, generates a signed QR boarding pass
here, and the driver app scans it via `POST /boarding/scan`, charging the
same ledger this app's wallet reads.**

## Running it

```powershell
# Terminal 1 — Postgres must already be up (infra/docker-compose.yml) and
# native Windows Postgres NOT shadowing port 5432 (see root CLAUDE.md).
cd backend
go run ./cmd/seed     # idempotent
go run ./cmd/server   # backend on :8080
```

```powershell
# Terminal 2 — commuter app dev server
cd apps/commuter
npm install
npm run dev           # http://localhost:5175
```

```powershell
# Terminal 3 (optional) — bring a seeded driver's vehicle online on a route
# so the Board screen's active-trip vehicle list and the loop-closing scan
# below have something real to show. Substitute the driver JWT/route id
# printed by cmd/seed or fetched via /auth/login and /routes.
cd backend
go run ./cmd/wsdriver -token <driver-jwt> -route <route-id>
```

Sign in at `http://localhost:5175` as a seeded commuter (e.g. `+27820000004`
/ `Commuter123!`), top up on the **Wallet** tab, then generate a boarding
pass on the **Board** tab. To close the loop by hand, copy the pass's raw
token (via "No camera? Show raw token") and either paste it into the driver
app's (`apps/driver`, port 5174) Scan screen manual-entry field while signed
in as the matching online driver, or `POST` it directly:

```powershell
Invoke-RestMethod -Method Post http://localhost:8080/boarding/scan `
  -Headers @{Authorization = "Bearer $driverToken"} -ContentType "application/json" `
  -Body (@{pass_token = $token} | ConvertTo-Json)
```

## Screens

1. **Login** — phone + password, same `AuthContext`/`sessionStorage` pattern
   as the driver app (Stage 9a): rejects a non-commuter role client-side, no
   `localStorage` for the token.
2. **Live map** (`Live` tab, the hero screen) — a Leaflet map centered on Cape
   Town. Pick a route and it opens `GET /ws/commuter?route_id=<id>`
   (deliberately unauthenticated, per Stage 4 — a commuter can watch the map
   before logging in were this screen reachable pre-auth): the initial
   `snapshot` populates markers, `update`/`offline` events move/remove them
   live. Each marker shows `seats_available`. A dropped connection shows a
   "Reconnecting" flap tile and re-subscribes with backoff (1s/2s/5s/8s,
   mirroring the driver app's socket hook). No driver online on the selected
   route → an honest "No vehicles on this route right now" empty state, never
   a blank/broken map.
3. **Search** (`Search` tab) — origin/destination pickers populated from real
   seeded stops, `GET /routes/search?from=&to=`. Direct results and
   one-transfer results are both rendered as an ordered stop list with a
   "Transfer at `<stop>`" marker between segments, plus the total fixed fare.
   A 404 (no path) renders as a clean "No route found" message, not an error
   screen.
4. **Routes** (`Routes` tab) — every route, tap through to its ordered stops
   and per-leg fares (`GET /routes/{id}`). Reachable directly from the tab bar
   or by tapping a segment in a search result.
5. **Wallet** (`Wallet` tab, Stage 9b-ii) — `GET /wallet/balance` in Rands,
   plus a demo top-up (`POST /wallet/topup {amount_cents}`, preset R20/R50/R100
   buttons or a custom Rand amount), clearly labelled as a **simulated
   top-up — there is no real payment gateway anywhere in this MVP** (same
   Stage 2 mechanism the backend has always had). A session-local list of
   top-ups made from this device is shown underneath; there is **no backend
   endpoint exposing a commuter's full transaction history** (only the owner
   dashboard's `/owner/ledger`, Stage 8, sees the whole ledger), so this list
   is honestly scoped to "top-ups you made this session," not a real
   statement. A "Refresh" button re-fetches the balance — useful right after
   a driver scans your boarding pass elsewhere, since this screen has no
   push channel of its own.
6. **Board** (`Board` tab, Stage 9b-ii, the loop-closer) — pick a **route**,
   then a **from/to stop pair on that route** (in physical sequence order —
   the same constraint `routing.FareForSegment` enforces server-side), then
   "Generate boarding pass" calls `POST /boarding/pass`. The response's
   `pass_token` is rendered **as a QR code** (`qrcode.react`) large and
   centered — the object a driver's camera scans via the driver app's Scan
   screen. Alongside it: a live countdown to `expires_at` (the pass is
   short-lived, Stage 5's ~3 minute TTL) with a "Valid"/"Expired" rubber-stamp
   verdict, the fixed fare, the route/stop pair in plain language, and — for
   camera-less desktop/dev testing — the raw `pass_token` string behind a
   `<details>` disclosure with a copy-to-clipboard button, matching how the
   driver app's Scan screen accepts a pasted token. Once expired, a
   "Generate new pass" button re-issues a pass for the same trip. Below the
   pass: a light **active-trip** summary (trip status, route) plus, if any
   vehicles happen to be online on that route right now (reusing the same
   `useRouteVehicles`/`/ws/commuter` hook the Live map uses), their live seat
   counts — honestly **not** tied to the specific vehicle that will end up
   scanning this pass, since telemetry (Stage 4) has no concept of "the
   vehicle assigned to this commuter's trip." Below that, a small
   **request-a-pickup** widget (`POST /stops/request {route_id, stop_id}`,
   Stage 6's commuter-side counterpart) lets the commuter alert the nearest
   approaching driver on the route, showing the real `driver_available`
   result rather than pretending it always succeeds.

## Data notes

- **No `GET /stops` endpoint exists** (Stage 3 only ever needed a stop by id
  or exact name). Rather than add a backend endpoint for a frontend-only
  stage, `src/hooks/useRoutesData.ts` derives the full stop list itself: it
  fetches every route once via `GET /routes` + `GET /routes/{id}` and
  de-duplicates the stops named in each route's legs. This is a one-time
  fetch on load, fine at this dataset's size; it would need a real endpoint
  if the route count grew large.
- **Leaflet tiles are fetched from OpenStreetMap over the internet** — the
  one online dependency in an otherwise fully-local app. No connection means
  a blank map (tile layer only; markers/UI chrome still render).
- Vehicle marker positions come from live telemetry (Stage 4) via the
  commuter WebSocket, not a poll — there is no periodic REST refresh on the
  map screen.
- **No backend endpoint exposes a commuter's own transaction history**
  (Stage 9b-ii) — the Wallet screen's "recent top-ups" list is a
  session-local client array, not ledger-derived, and says so on-screen.
  Only `/owner/ledger` (Stage 8, owner-only) sees the full ledger.
- **The Board screen's active-trip view is a route-wide vehicle list, not a
  single-vehicle tracker** (Stage 9b-ii) — there is no backend concept
  linking a specific boarding pass to the specific vehicle that will scan
  it, so it shows whichever vehicles happen to be online on the pass's
  route, honestly labelled as such rather than faked as "your driver's
  location."
- **A boarding pass is scoped to one route** — from/to stop selects on the
  Board screen only ever offer stops on the currently-selected route, in
  increasing physical sequence order, matching what
  `routing.FareForSegment` (Stage 3/5) requires server-side. There is no
  cross-route trip-planning integration between the Search tab's
  multi-segment results and pass generation in this stage.

## Design direction

The driver app's signature object is the windscreen destination board — read
from inside a cab, at night, on shift. The commuter reads the exact same
board, but from the rank, at street level, in daylight, waiting rather than
working. Rather than reusing the driver app's palette wholesale (which would
make the commuter app look like a reskinned driver dashboard) or inventing an
unrelated visual language, this app keeps the one object both apps
genuinely share — the cream-and-ink destination board (`.board` in
`src/index.css`, same construction as the driver app's) — and rebuilds
everything *around* it for a different time of day and a different posture:

- **`dawn`** — a warm parchment daylight backdrop, replacing the driver app's
  dark `tar`. The commuter app is light-first; the driver app is dark-first.
  That's a deliberate inversion, not an oversight: one is a night shift
  dashboard, the other is midday rank/street level.
- **`marigold`** — the rank-wall paint colour, primary action. A relative of
  the driver app's curb-paint `rank` yellow, but the wall you're standing
  against rather than the paint at your feet.
- **`transit`** teal — live vehicles/motion on the map and the "watching a
  route" state. Deliberately *not* the driver app's livery `taxi` blue, so a
  commuter never reads "a taxi's paint colour" where the app means "this
  marker is moving right now."
- **`flag`** red — no-route / disconnected states only, the commuter
  counterpart to the driver app's `brake` red.
- **`.flap`** — the commuter app's one new signature surface: a split-flap
  departures tile, styled like the physical board bolted above a real taxi
  rank. It's the commuter-side equivalent of the driver app's `.led`
  dashboard instrument lights, but reads like waiting for a board to flip
  over rather than checking a dashboard gauge — the right physical metaphor
  for someone standing at a rank, not driving a cab.
- **`.ticket`** — the search result's fare slip is the same dashed-border,
  punch-hole ticket construction as the driver app's boarding receipt, since
  it's the same underlying object: a printed journey summary.
- **Vehicle markers** on the map are small taxi-board chips (`.vehicle-marker`)
  rather than Leaflet's default pin, so the map's one live-data surface stays
  inside the same visual language as the rest of the app.
- **`.stamp`** (Stage 9b-ii) — the boarding pass's own rubber-stamp verdict
  ("Valid" in `transit` teal / "Expired" in `flag` red), the commuter-side
  counterpart to the driver app's "Paid"/"Already Paid" scan-receipt stamp —
  here reporting the pass's own validity rather than a charge outcome, since
  the commuter never sees the charge itself (the driver app does).

## Verified

`npm install`, `npx tsc --noEmit`, and `npm run build` all pass cleanly.

**Stage 9b-i** (map/search/routes): ran Postgres + `cmd/server` + `npm run dev`
together and drove the real app with Playwright (Chromium): logged in as the
seeded commuter (`+27820000004` / `Commuter123!`), watched the live map show a
clean "no vehicles" empty state, brought a seeded driver online on a route via
`cmd/wsdriver` and confirmed a real marker (with live seat count) appeared on
the map within the WS's snapshot/update flow, ran a direct search
(Cape Town Station → Khayelitsha Town Centre), a one-transfer search
(Khayelitsha Town Centre → Wynberg, transfer at Athlone), a genuine no-path
search (Khayelitsha Town Centre → Muizenberg), and opened a route's detail
view both from a search result segment and from the Routes tab directly.

**Stage 9b-ii** (wallet/boarding-pass/active-trip — the cross-app loop):
brought seeded driver 1's vehicle (`CA123456`) online on "Cape Town CBD -
Bellville" via `cmd/wsdriver`, then drove the real commuter app with
Playwright as the seeded commuter:
- Wallet: screenshotted the balance (R3963.00), tapped the R100 preset
  top-up, and screenshotted the updated balance (R4063.00) — a real
  `POST /wallet/topup` round-trip, not a mocked number.
- Board: selected route "Cape Town CBD - Bellville", from stop "Cape Town
  Station", to stop "Bellville Station", generated a boarding pass
  (`POST /boarding/pass`, fare R11.00 = 700 + 400 cents per Stage 3's seeded
  legs), and screenshotted the rendered QR code, the live countdown ticking
  down (2:59 → 2:57 across two screenshots a second apart, proving it's a
  real timer, not a static label), and the "Valid" stamp. Revealed and
  copied the raw `pass_token` fallback.
- **Closed the loop end-to-end**: took the exact `pass_token` the commuter
  app rendered and `POST`ed it to `/boarding/scan` as the seeded driver
  (`+27820000002`) via a script — the same act the driver app's Scan screen
  performs on camera decode. Confirmed a **fresh charge**
  (`replayed: false`, fare 1100 cents, split 110/275/715, `seats_remaining`
  16→15) and that the commuter's `GET /wallet/balance` dropped from
  **R4063.00 to R4052.00** — verified both directly via the API and by
  reloading the commuter app's Wallet screen and screenshotting the same
  R4052.00 balance rendered in the real UI.
- Request-a-stop: from the generated pass's Board screen, requested a
  pickup at "Cape Town Station" (`POST /stops/request`) and confirmed the
  real `driver_available: true` result rendered ("A nearby driver has been
  alerted to your stop.") — the commuter-side counterpart to the driver app
  receiving that alert over `/ws/driver` (Stage 6).

All screenshots were taken against the live backend (no mocked responses) —
Playwright was driven via `playwright-core` pointed at the machine's cached
Chromium build, since a fresh `npx playwright install` wasn't available in
this environment.
