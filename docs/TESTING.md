# Ses'fikile — manual test checklist

A step-by-step, click-through script for verifying the whole system by hand:
backend + all three frontend apps (driver, commuter, owner) working together.
Written for someone following along at a keyboard, not someone who already
knows the codebase.

You'll need two browser windows/tabs open side by side for most of this: one
logged in as the **driver**, one as the **commuter**. A third (or a separate
browser profile) for the **owner** at the end.

---

## 1. Prerequisites

- Docker Desktop installed and running.
- Go 1.22+ and Node.js installed.
- First time only: install frontend dependencies —
  ```powershell
  cd apps\driver;   npm install; cd ..\..
  cd apps\commuter; npm install; cd ..\..
  cd apps\owner;    npm install; cd ..\..
  ```

## 2. One-command start

From the repo root:

```powershell
.\scripts\dev-up.ps1
```

This starts Postgres (Docker), seeds dev data, and opens four new PowerShell
windows — one each for the backend and the three frontend apps — leaving
their logs visible. Wait for the script to print the final URL summary:

```
Backend:  http://localhost:8080
driver    http://localhost:5174
commuter  http://localhost:5175
owner     http://localhost:5176
```

If anything fails, the script prints a specific error (Docker not running,
port 5432 already in use by a native Postgres service, missing
`node_modules`, etc.) — see **Troubleshooting** at the end of this document.

When you're done testing, run `.\scripts\dev-down.ps1` (see the end of this
document).

## 3. Seeded logins

`cmd/seed` prints these to the console every run (the backend PowerShell
window, near the top, under "SEEDED DATA"). They're stable across runs:

| Role       | Phone           | Password      |
|------------|-----------------|---------------|
| Owner      | `+27820000001`  | `Owner123!`   |
| Driver 1   | `+27820000002`  | `Driver123!`  |
| Driver 2   | `+27820000003`  | `Driver123!`  |
| Commuter 1 | `+27820000004`  | `Commuter123!`|
| Commuter 2 | `+27820000005`  | `Commuter123!`|

Driver 1 is assigned vehicle `CA123456`; driver 2 is assigned `CA654321`.
Both commuters start with a **R100.00** wallet balance. Every seeded route
runs between real Cape Town corridor names (e.g. "Cape Town CBD -
Khayelitsha", "Athlone - Wynberg") — pick any one for the walkthrough below.

---

## 4. THE CORE LOOP

Do these steps in order. Each one says what to click and what you should see.

### Step 1 — Driver logs in and goes online

1. Open `http://localhost:5174` (driver app).
2. Sign in with driver 1: phone `+27820000002`, password `Driver123!`.
3. On the dashboard, use the **"Choose today's route"** dropdown and pick any
   route. Picking it immediately goes online — there's no separate confirm
   step.
4. Your browser will ask for **location permission**. Click **Allow**. A
   status pill should read **"GPS locked"**. (If you click Block, it reads
   "Location blocked — check permissions" — allow it, or the live map step
   below won't work.)

**Expect:** the dashboard shows you as online on the chosen route, with a
GPS status pill and an "End shift — go offline" button.

### Step 2 — Commuter logs in and sees the driver's marker move

1. Open `http://localhost:5175` (commuter app) in a second window/tab.
2. Sign in with commuter 1: phone `+27820000004`, password `Commuter123!`.
3. Go to the **Map** screen. In the **"Watching route"** dropdown, pick the
   *same route* the driver just went online on.

**Expect:** a 🚐 marker appears on the map with a seat count next to it, and
the status flap reads "Live". If the driver's browser is genuinely updating
GPS position, the marker should visibly move over the next 10-30 seconds. A
blank map with no tiles usually just means no internet reachability to
OpenStreetMap (see Troubleshooting) — the marker/vehicle logic is independent
of whether tiles loaded.

### Step 3 — Commuter checks wallet balance

1. Go to the **Wallet** screen.

**Expect:** balance shows **R100.00** (commuter 1's seeded starting balance).
Note it exactly — you'll check it again after boarding.

### Step 4 — Commuter generates a boarding pass

1. Go to the **Board** screen (or wherever "Generate boarding pass" lives).
2. Pick the same **Route** the driver is online on, and a **From**/**To** stop
   pair along that route.
3. Click **"Generate boarding pass"**.

**Expect:** a ticket appears with a large **boarding code** as the hero
(grouped like `K7M2-9XQP`), a chunky/low-density QR beneath it (it now
encodes only that 8-character code, not the full signed token — noticeably
smaller/blockier than before this stage), a stamp reading "Valid", and a live
countdown (the pass expires ~3 minutes after issue — if it turns "Expired"
before you scan it, just generate a new one).

4. Note the boarding code (or click **"Copy code"** — it will read "Copied!"
   briefly). This is the **short-code fallback** the driver app needs when
   there's no camera pointed at the commuter's screen — the normal case on a
   single laptop, and the *only* option on a real phone-to-phone LAN demo
   (see Known Limitations below).

### Step 5 — Driver scans the pass (boarding code)

1. Switch to the driver app window. Go to the **Scan** screen.
2. Click **"Enter boarding code instead"** (skip the camera option — no
   camera is pointed at the commuter's screen).
3. Type the boarding code into the field (case doesn't matter, and the hyphen
   is optional) and click **"Charge fare"**.

**Expect:** a "ticket" result screen appears with a stamp reading **"Paid"**,
heading "Fare charged", the fare amount, and a breakdown of **Driver share /
Owner share / Platform fee / Seats remaining**. Note the fare and the new
seats-remaining number.

*(Dev-only alternative: the driver Scan screen's "Dev fallback: paste full
pass token" disclosure still accepts the raw `pass_token` directly, exactly
as before this stage — useful for testing the token path in isolation, not
part of the normal demo flow.)*

### Step 6 — Confirm the commuter's wallet dropped by exactly the fare

1. Switch back to the commuter app, go to **Wallet**, click **Refresh**.

**Expect:** balance is now exactly `(previous balance) - (fare charged)`. Not
approximately — exactly, to the cent.

### Step 7 — Owner sees the trip in revenue and the ledger

1. Open `http://localhost:5176` (owner app), sign in as the owner:
   `+27820000001` / `Owner123!`.
2. Check the **"Revenue vs Fuel"** tab — the fare's owner share should be
   reflected in revenue.
3. Check the **"Ledger"** tab — you should be able to find the transaction
   from Step 5, with postings that balance (debit from the commuter, credits
   to driver/owner/platform summing back to the fare).

---

## 5. The idempotency demo (no double charge)

This proves a replayed scan can't charge someone twice.

1. Back in the driver app's Scan screen, after Step 5 above, click **"Scan
   next pass"**, then enter the **exact same boarding code** again (try
   mixing the case or dropping the hyphen — it still resolves to the same
   pass) and click **"Charge fare"**.

**Expect:** the result screen's stamp now reads **"Already Paid"** with a
note that the commuter was *not* charged again. "Seats remaining" is
unchanged from the first scan. Go check the commuter's wallet balance again
— it should be identical to what it was right after Step 6, not lower again.

---

## 6. Request-a-stop

1. As the commuter, on the active boarding-pass view, find the **"Request a
   pickup"** panel. Pick a stop and click **"Request"**.

**Expect:** a confirmation message like "A nearby driver has been alerted to
your stop" (or a message that no driver is currently close enough/approaching
— this depends on the driver's simulated GPS position relative to the
requested stop).

2. Switch to the driver app's **"Stop requests"** screen (under the Alerts/
   Dispatch tab). If a driver was matched, an alert card should appear
   showing the stop name and request time.
3. Click **"Acknowledge"** on that card.

**Expect:** the button updates to **"Acknowledged ✓"**.

---

## 7. Optional: the real Cape Town catalogue coverage layer

By default the dev stack only has the clean 8-route/12-stop seeded baseline.
To also see the real ~1447-route City of Cape Town catalogue as a browsable
backdrop:

1. Stop the stack (`.\scripts\dev-down.ps1`) and restart it with
   `.\scripts\dev-up.ps1 -WithCatalogue` (requires
   `backend/data/taxi_routes.json` locally — see `backend/data/README.md` if
   it's missing; the script will warn and continue without it rather than
   fail).
2. In the commuter app's Map screen, switch on the **"Network coverage"**
   toggle.

**Expect:** thin dashed lines fill in across the whole city (this can take
a second to fetch/render), plus a legend explaining "Live routes" (solid,
vehicles running now) vs. "Network coverage" (dashed, real route data, no
live vehicles, fares estimated).

3. In **Search**, look up a trip between two catalogue-only ranks (stop
   pickers group options into "Live ranks" vs. "Network coverage (browse
   only)" optgroups — pick from the second group for both ends).

**Expect:** the result shows a muted **"Coverage"** badge, an **"Est. total
fare"** label instead of "Total fare", and no option to generate a boarding
pass for it (catalogue routes are browse-only — there's never a real vehicle
on one, so a pass could never be scanned).

---

## 8. KNOWN LIMITATIONS (read before reporting something as broken)

- **Camera QR scanning and GPS geolocation still require a secure context**
  (`localhost` or `https`). Both work fine over `http://localhost:...` on the
  same machine (what this checklist uses), but **will not work** if you open
  the app from a phone over your LAN at `http://<your-pc-ip>:5175` — the
  browser will silently refuse camera/location access. **This no longer
  blocks the core boarding demo**, though: the boarding **code** (Step 4/5
  above) is a typeable/speakable fallback specifically designed for this case
  — read the code off the commuter's phone screen and type it into the
  driver's Scan screen, no camera permission needed on either device. (GPS
  geolocation for the driver's live position is a separate permission and
  still needs a secure context or a manually-entered position if testing
  that specific feature over LAN.)
- **Catalogue routes are browse-only.** They will never show a live vehicle,
  never offer a boarding pass, and their fares are distance-estimated, not
  real association tariffs.
- **Fuel/VIU hardware is entirely mocked** — there is no real fuel dispenser
  integration anywhere in this MVP.
- Vehicle positions and stop-request state are **in-memory only** — they
  reset if the backend server restarts (this is intentional for the MVP, not
  a bug).

## 9. Troubleshooting

| Symptom | Likely cause |
|---|---|
| Blank/grey map tiles | No internet access to OpenStreetMap's tile servers. The rest of the app (markers, wallet, boarding) works independently of tile loading. |
| No vehicle marker on the commuter map | No driver is currently online on the *same* route you're watching — double check both apps picked the identical route. |
| Owner dashboard shows all zeros | No fares have been charged yet this session — complete at least one scan (Step 5) first. |
| `docker compose up` fails / Postgres never becomes healthy | Something else (often a native Windows PostgreSQL service) is already bound to port 5432. `scripts/dev-up.ps1` checks for this and will tell you the offending process — stop it and re-run. |
| A frontend window shows `Cannot find module` or similar on `npm run dev` | `node_modules` wasn't installed for that app — run `npm install` inside `apps\<name>` and re-run `scripts\dev-up.ps1`. |

---

## Tearing down

```powershell
.\scripts\dev-down.ps1
```

Stops all four dev-server windows and the Postgres container, keeping your
seeded data on disk (the Docker volume persists).

To wipe the database completely and start from a genuinely empty volume next
time:

```powershell
.\scripts\dev-down.ps1 -WipeDb
```

This asks for a typed confirmation first (it is destructive — every seeded
and imported row is gone).
