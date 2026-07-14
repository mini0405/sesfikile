# Ses'fikile — commuter app (Stage 9b-i)

Vite + React 18 + TypeScript (strict) + Tailwind. Dev server on port **5175**
(backend on 8080, driver app on 5174). `VITE_API_BASE_URL` in `.env`/`.env.example`
(default `http://localhost:8080`).

This is the **first half** of the commuter app: sign-in, the live vehicle map,
route search, and route detail. Wallet, boarding-pass generation, and the
active-trip screen are Stage 9b-ii.

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

## Verified

`npm install`, `npx tsc --noEmit`, and `npm run build` all pass cleanly.
Ran Postgres + `cmd/server` + `npm run dev` together and drove the real app
with Playwright (Chromium): logged in as the seeded commuter
(`+27820000004` / `Commuter123!`), watched the live map show a clean "no
vehicles" empty state, brought a seeded driver online on a route via
`cmd/wsdriver` and confirmed a real marker (with live seat count) appeared on
the map within the WS's snapshot/update flow, ran a direct search
(Cape Town Station → Khayelitsha Town Centre), a one-transfer search
(Khayelitsha Town Centre → Wynberg, transfer at Athlone), a genuine no-path
search (Khayelitsha Town Centre → Muizenberg), and opened a route's detail
view both from a search result segment and from the Routes tab directly.
