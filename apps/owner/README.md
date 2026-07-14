# Ses'fikile — owner dashboard (Stage 9c)

Vite + React 18 + TypeScript (strict) + Tailwind + Recharts. Dev server on
port **5176** (backend on 8080, driver app on 5174, commuter app on 5175).
`VITE_API_BASE_URL` in `.env`/`.env.example` (default `http://localhost:8080`).

Frontend only, no backend changes — built entirely on Stage 8's read-only
`/owner/*` analytics endpoints. Same `AuthContext`/typed-`fetch`-client
pattern as the driver (Stage 9a) and commuter (Stage 9b) apps, no state
library.

## Desktop-first, not mobile-first

This is the one Ses'fikile frontend that's **desktop-first and data-dense**:
an owner reviews their business on a laptop at the end of the day, not
one-handed on a phone at a rank. That changes the layout, not the identity:

- A persistent **left sidebar** for navigation (Overview / Revenue vs Fuel /
  Fleet / Drivers / Ledger), not a phone bottom-tab bar — desks have a left
  margin to spare; thumbs don't.
- A **content grid of cards, tables, and a chart**, not a single scrollable
  card stack — an owner is comparing several numbers at once, not swiping
  through one at a time.
- **No camera, no GPS, no QR, no WebSocket** — this app never drives a
  vehicle or boards a commuter. It only reads `GET /owner/*`.
- Tables use **tabular numerals** (`.num`, `font-mono tabular-nums`) so
  figures align in a column the way they would on a printed statement —
  legibility for scanning down a column of Rand amounts matters more here
  than anywhere else in the app suite.
- The page still reflows down to a narrower viewport (the sidebar and grid
  both use relative/flex sizing), but it is not designed around a phone
  screen the way the other two apps are.

## Screens

1. **Login** — phone + password, same `AuthContext`/`sessionStorage` pattern
   as the driver/commuter apps: rejects a non-owner role client-side, no
   `localStorage` for the token. Seeded owner: `+27820000001` / `Owner123!`.
2. **Overview** (`GET /owner/summary?from=&to=`) — headline stat cards:
   revenue, trips, passenger volume, platform fees, driver earnings paid,
   fuel account balance, fuel allocated for the range.
3. **Revenue vs Fuel** (`GET /owner/revenue-vs-fuel?from=&to=`) — the
   dashboard's signature view. Headline totals (revenue, fuel allocated,
   fuel consumed, and a "fuel share of revenue" ratio computed only for
   display) above a Recharts `ComposedChart`: grouped bars for revenue vs
   fuel allocated per day, plus a dashed line for fuel consumed. The Y axis
   always starts at zero and is labelled in Rands — no truncated or
   misleadingly-scaled axis.
4. **Fleet** (`GET /owner/vehicles?from=&to=`) — one row per vehicle:
   assigned driver, live online/offline + current route (Stage 4 telemetry,
   right now — not a historical log), seats, trips/revenue for the range,
   and fuel quota (available / total).
5. **Drivers** (`GET /owner/drivers?from=&to=`) — one row per driver:
   assigned vehicle, live online status, trips/earnings for the range.
6. **Ledger** (`GET /owner/ledger?from=&to=&limit=&offset=`) — the
   transparency/anti-skimming view made visible: every fare crediting the
   owner's revenue account (with the platform/driver/owner split spelled
   out), every fuel withholding, and every fuel-pump authorization against
   the fleet, in one paginated, chronological table.

## Date range

A single date-range control (Today / Last 7 days / Last 30 days / Custom)
lives in the top bar and every screen respects it — lifted once in
`OwnerApp.tsx`, not duplicated per screen. Each screen echoes back the exact
`from`/`to` the API response itself carries (`ActiveRangeNote`), not the
picker's own guess, and notes that the backend's "today"/date-range boundary
is anchored to a fixed **Africa/Johannesburg** timezone (Stage 8's
`internal/analytics/daterange.go`) — the client-side preset math ("7 days
ago") is only an approximation used to pick a `from=` value to send; the
range actually applied is whatever the response echoes.

`GET /owner/ledger`'s response has no `from`/`to` fields (see
`internal/analytics/models.go`'s `LedgerPage`), so the Ledger screen doesn't
render an echoed-range note the way the other four screens do — it still
sends the same `from`/`to` as everyone else, just can't display the
server's own confirmation of them.

## Integrity note — every figure is read straight off the ledger

Per CLAUDE.md's non-negotiable ("the ledger is double-entry and ACID") and
the Stage 8 brief: **this dashboard displays Stage 8's ledger-reconciled
figures, it never recomputes or adjusts them.** Every monetary value on
every screen is rendered directly from the corresponding `/owner/*` response
field — `formatRand()` (`src/format.ts`) only converts cents to a Rand
string for display, it never changes the underlying number. The one
exception, called out on-screen where it appears (Revenue vs Fuel's "fuel
share of revenue"), is a **percentage ratio computed for display only** from
two already-fetched authoritative totals — the two source figures it divides
are shown unmodified right next to it, so the ratio can never disagree with
them. The sidebar's "✓ Ledger-reconciled" stamp is this app's one design
callout of that same claim.

## Design direction

The driver app's signature object is the windscreen destination board; the
commuter app's is the same board read from the rank. This app's signature
object is different again: **the association's own ledger book**, read from
behind the counter at the back office, not from the cab or the rank. An
owner isn't tapping a board or holding up a boarding pass — they're closing
out the books.

- **`paper`** — a cooler, quieter off-white than the commuter app's warm
  midday `dawn`, because this is an indoor back-office screen, not a rank at
  street level.
- **`brass`** — a desaturated relative of the driver app's curb-paint `rank`
  yellow and the commuter app's `marigold`, toned down for a professional
  register: an accent for the one action per screen (the active date-range
  pill, primary buttons), not a loud street-level call to action.
- **`signal`** teal / **`alert`** red — the same online/offline and
  positive/negative relatives as the other two apps' `transit`/`flag` and
  `taxi`/`brake` pairs, desaturated to match.
- **`.ledger-card`** — the same cream-cardstock-and-ink-rule construction
  family as the other apps' `.board`, but flat and dense rather than
  taped-and-tilted: read at a desk, not tapped to a windscreen.
- **`.stamp-reconciled`** — the same rubber-stamp lineage as the driver
  app's Paid/Already-Paid stamp and the commuter app's Valid/Expired stamp,
  here repurposed for the back office's own signature claim: every figure on
  this screen came straight from the ledger.
- Tables use ruled, dense rows (`table.ledger-table`) rather than card-based
  lists — the register's actual physical object is a ruled ledger page, not
  a stack of tickets.

## Running it

```powershell
# Terminal 1 — Postgres must already be up (infra/docker-compose.yml) and
# native Windows Postgres NOT shadowing port 5432 (see root CLAUDE.md).
cd backend
go run ./cmd/seed     # idempotent
go run ./cmd/server   # backend on :8080
```

```powershell
# Terminal 2 — owner dashboard dev server
cd apps/owner
npm install
npm run dev           # http://localhost:5176
```

Sign in as the seeded owner: `+27820000001` / `Owner123!`.

**Meaningful numbers require some activity first** — a freshly-seeded owner
with no fares charged and no fuel allocated will correctly show clean zeros
everywhere (that's the honest empty state, not a bug). To populate the
dashboard, charge a few fares and allocate fuel first, reusing the earlier
stages' own commands (Stage 2/5's `/fare/charge` or `/boarding/scan`, and
Stage 7's `/fuel/allocate`):

```powershell
# Log in as the seeded owner, driver 1, and commuter 1.
$ownerToken = (Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000001","password":"Owner123!"}').token
$driverToken = (Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000002","password":"Driver123!"}').token
$commuterId = (Invoke-RestMethod -Method Post http://localhost:8080/auth/login `
  -ContentType "application/json" -Body '{"phone":"+27820000004","password":"Commuter123!"}').user_id

# vehicleId = CA123456's id, printed by cmd/seed
$vehicleId = "<paste CA123456's id from cmd/seed output>"

# Charge a couple of fares so owner_revenue is non-zero.
Invoke-RestMethod -Method Post http://localhost:8080/fare/charge `
  -Headers @{Authorization = "Bearer $driverToken"} -ContentType "application/json" `
  -Body (@{commuter_id=$commuterId; vehicle_id=$vehicleId; fare_cents=3500; idempotency_key=[guid]::NewGuid().ToString()} | ConvertTo-Json)

# Withhold fuel from revenue (Stage 7) so Revenue vs Fuel has something to show.
Invoke-RestMethod -Method Post http://localhost:8080/fuel/allocate `
  -Headers @{Authorization = "Bearer $ownerToken"}
```

Then reload the Overview / Revenue vs Fuel / Fleet / Drivers / Ledger
screens — the figures should now be non-zero and reconcile with each other
(e.g. Overview's revenue matches the sum of Ledger's `fare` entries'
owner-share amounts for the same range).

## Verified

`npm install`, `npx tsc --noEmit`, and `npm run build` all pass cleanly.
Ran Postgres + `cmd/server` + `npm run dev` together, seeded, charged five
fares across both seeded vehicles, ran `/fuel/allocate` + funded a vehicle
quota + authorized and confirmed a mock VIU pump session, then drove the
real app with Playwright (`playwright-core` against the machine's cached
Chromium) as the seeded owner:

- Screenshotted the Overview stat cards (non-zero revenue/trips/fees/fuel
  figures), the Revenue vs Fuel chart (a zero-based, Rand-labelled grouped
  bar chart with a dashed fuel-consumed line), the Fleet table (both
  vehicles, trips/revenue/fuel quota populated), the Drivers table, and the
  Ledger table (fare/fuel_allocation/fuel_authorization rows, oldest-first
  pagination, the owner/driver/platform split spelled out per fare row).
- Switched the date-range preset (Today → Last 7 days) and confirmed every
  screen's echoed `from`/`to` and figures updated together.
- **Found and fixed a real bug during this pass**: the backend serializes a
  Go nil slice as JSON `null` (not `[]`) when a ledger range has zero
  entries — `internal/analytics/repo.go`'s `Ledger` declares `var entries
  []LedgerEntry` rather than initializing it with `make`, unlike
  `Vehicles`/`Drivers`, which do use `make([]T, 0, ...)` and so always
  return `[]`. The Ledger screen crashed reading `.length` off that `null`.
  Fixed by normalizing `entries ?? []` right after the fetch in
  `LedgerScreen.tsx`, rather than changing the backend (Stage 9c is
  frontend-only) — this exact shape was already called out as the correct,
  expected response in Stage 8's own scoping-test notes in
  `docs/PROGRESS.md` ("`{"entries": null, "total": 0, ...}`"), so the
  frontend needed to handle it, not treat it as a backend defect.
- **Owner scoping, verified visually**: registered/reused a second, wholly
  unrelated owner (`+27820099999` / `Owner2Pass!`) and confirmed every
  screen — Overview, Fleet, and (after the fix above) Ledger — shows clean
  zeros / empty states, never owner 1's revenue, vehicles, or ledger rows.
  This matches Stage 8's own `TestScoping_OwnerCannotSeeAnotherOwnersData`
  guarantee, now visible end-to-end through the real UI.
