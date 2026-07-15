import { useMemo, useState } from "react";
import { QRCodeSVG } from "qrcode.react";
import { api, ApiError } from "../api/client";
import { useCountdown } from "../hooks/useCountdown";
import { useRouteDetail } from "../hooks/useRouteDetail";
import { useRouteVehicles } from "../hooks/useRouteVehicles";
import type { Route, RouteDetail } from "../types";

interface BoardScreenProps {
  routes: Route[];
  loading: boolean;
  error: string | null;
  getRouteDetail: (routeId: string) => Promise<RouteDetail | null>;
}

interface OrderedStop {
  id: string;
  name: string;
}

/** A route's stops in physical order — first leg's origin, then each leg's
 * destination in sequence. Same derivation RoutesScreen already uses to
 * render a route's stop list; needed here so from/to selects only ever offer
 * a valid, increasing-sequence pair (what routing.FareForSegment requires). */
function orderedStops(detail: RouteDetail): OrderedStop[] {
  if (detail.legs.length === 0) return [];
  const stops: OrderedStop[] = [{ id: detail.legs[0].from_stop_id, name: detail.legs[0].from_stop_name }];
  for (const leg of detail.legs) {
    stops.push({ id: leg.to_stop_id, name: leg.to_stop_name });
  }
  return stops;
}

interface IssuedPass {
  token: string;
  expiresAt: string;
  fareCents: number;
  routeId: string;
  routeName: string;
  fromName: string;
  toName: string;
}

export function BoardScreen({ routes, loading, error, getRouteDetail }: BoardScreenProps) {
  // Only ever offer live (seeded) routes here — a catalogue route has no
  // vehicle/driver data connecting it to anything, so a pass issued against
  // one could never be scanned. Rather than let a commuter generate a pass
  // that can structurally never be honoured, catalogue routes simply never
  // appear in this picker (see CLAUDE.md's "never visually confusable" core
  // principle for this stage — the honest move here is omission, not a
  // disabled option with an asterisk).
  const liveRoutes = useMemo(() => routes.filter((r) => r.source !== "catalogue"), [routes]);

  const [routeId, setRouteId] = useState("");
  const [fromStopId, setFromStopId] = useState("");
  const [toStopId, setToStopId] = useState("");
  const [issuing, setIssuing] = useState(false);
  const [issueError, setIssueError] = useState<string | null>(null);
  const [pass, setPass] = useState<IssuedPass | null>(null);
  const [copied, setCopied] = useState(false);

  const [stopRequestStopId, setStopRequestStopId] = useState("");
  const [stopRequestBusy, setStopRequestBusy] = useState(false);
  const [stopRequestResult, setStopRequestResult] = useState<{ available: boolean; message: string } | null>(null);

  const { detail } = useRouteDetail(routeId || null, getRouteDetail);
  const stops = useMemo(() => (detail ? orderedStops(detail) : []), [detail]);
  const fromIndex = stops.findIndex((s) => s.id === fromStopId);

  const countdown = useCountdown(pass?.expiresAt ?? null);
  const { vehicles } = useRouteVehicles(pass?.routeId ?? null);
  const { detail: passDetail } = useRouteDetail(pass?.routeId ?? null, getRouteDetail);

  function selectRoute(id: string) {
    setRouteId(id);
    setFromStopId("");
    setToStopId("");
    setIssueError(null);
  }

  // routeId/fromStopId/toStopId stay set once a trip is picked (changeTrip()
  // only clears the issued pass, not the selection), so regenerating an
  // expired pass for the same trip is just calling this again with no args.
  async function generatePass() {
    if (!routeId || !fromStopId || !toStopId) return;

    setIssuing(true);
    setIssueError(null);
    try {
      const res = await api.issuePass(routeId, fromStopId, toStopId);
      const route = routes.find((r) => r.id === routeId);
      const fromStop = stops.find((s) => s.id === fromStopId);
      const toStop = stops.find((s) => s.id === toStopId);
      setPass({
        token: res.pass_token,
        expiresAt: res.expires_at,
        fareCents: res.fare_cents,
        routeId,
        routeName: route?.name ?? "",
        fromName: fromStop?.name ?? "",
        toName: toStop?.name ?? "",
      });
      setCopied(false);
    } catch (err) {
      setIssueError(err instanceof ApiError ? err.message : "Could not generate a boarding pass. Try again.");
    } finally {
      setIssuing(false);
    }
  }

  function changeTrip() {
    setPass(null);
    setIssueError(null);
  }

  async function copyToken() {
    if (!pass) return;
    try {
      await navigator.clipboard.writeText(pass.token);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard API unavailable — the raw token is already selectable text
    }
  }

  async function requestPickup(stopId: string) {
    if (!pass || !stopId) return;
    setStopRequestBusy(true);
    setStopRequestResult(null);
    try {
      const res = await api.requestStop(pass.routeId, stopId);
      setStopRequestResult({
        available: res.driver_available,
        message: res.driver_available
          ? "A nearby driver has been alerted to your stop."
          : res.message ?? "No driver is currently available for this stop.",
      });
    } catch (err) {
      setStopRequestResult({
        available: false,
        message: err instanceof ApiError ? err.message : "Could not send the request. Try again.",
      });
    } finally {
      setStopRequestBusy(false);
    }
  }

  // ---- The boarding-pass / active-trip view, once a pass has been issued ----
  if (pass) {
    const pickupStops = passDetail ? orderedStops(passDetail) : [];

    return (
      <div className="mx-auto max-w-md px-4 pb-24 pt-6">
        <div className="mb-3 flex items-center justify-between">
          <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Boarding pass</p>
          <button
            onClick={changeTrip}
            className="text-xs font-bold uppercase tracking-wide text-dawn-400 active:text-marigold-700"
          >
            Change trip
          </button>
        </div>

        {/* The hero object: the QR a driver's camera scans. */}
        <div className="ticket mb-4 text-center">
          <span className={`stamp mb-3 ${countdown.expired ? "stamp-expired" : "stamp-live"}`}>
            {countdown.expired ? "Expired" : "Valid"}
          </span>

          {countdown.expired ? (
            <div className="py-8">
              <p className="board-value mb-2">Pass expired</p>
              <p className="mb-4 text-sm text-ink/70">
                Boarding passes are short-lived for security. Generate a fresh one for the same trip.
              </p>
              <button onClick={() => void generatePass()} disabled={issuing} className="btn-marigold">
                {issuing ? "Generating…" : "Generate new pass"}
              </button>
            </div>
          ) : (
            <>
              <div className="mx-auto mb-4 inline-block rounded-sm border-2 border-ink bg-white p-3 shadow-tape">
                <QRCodeSVG value={pass.token} size={220} bgColor="#ffffff" fgColor="#201C16" level="M" />
              </div>

              <p className="font-display text-3xl font-black tabular-nums text-ink">{countdown.label}</p>
              <p className="mb-4 text-xs uppercase tracking-wide text-ink/50">until this pass expires</p>

              <div className="mb-4 border-y border-dashed border-ink/30 py-3 text-left">
                <p className="board-heading mb-1">{pass.routeName}</p>
                <p className="text-sm font-bold text-ink">
                  {pass.fromName} → {pass.toName}
                </p>
              </div>

              <div className="mb-4 flex items-center justify-between">
                <p className="board-heading">Fare</p>
                <p className="font-display text-2xl font-black">R{(pass.fareCents / 100).toFixed(2)}</p>
              </div>

              <details className="text-left">
                <summary className="cursor-pointer text-xs font-bold uppercase tracking-wide text-ink/50">
                  No camera? Show raw token
                </summary>
                <div className="mt-2 space-y-2">
                  <p className="break-all rounded-sm border border-dashed border-ink/30 bg-board-dim px-2 py-2 font-mono text-[10px] text-ink/70">
                    {pass.token}
                  </p>
                  <button onClick={() => void copyToken()} className="btn-ghost">
                    {copied ? "Copied!" : "Copy token"}
                  </button>
                </div>
              </details>
            </>
          )}
        </div>

        {/* Active trip — light MVP: a trip summary plus, if available, any
            vehicles currently online on this route. Not tied to the specific
            vehicle that will scan this pass — telemetry (Stage 4) has no
            concept of "the vehicle assigned to this commuter's trip", so this
            is honestly a route-wide view, not a single-vehicle tracker. */}
        <div className="board relative px-4 pb-4 pt-6">
          <span className="tape left-6" />
          <p className="board-heading mb-2">Active trip</p>
          <div className="mb-3 space-y-1 text-sm">
            <p className="text-ink/70">
              Status: <span className="font-bold text-ink">{countdown.expired ? "Pass expired" : "Ready to board"}</span>
            </p>
            <p className="text-ink/70">
              Route: <span className="font-bold text-ink">{pass.routeName}</span>
            </p>
          </div>
          {vehicles.length === 0 ? (
            <p className="text-xs text-ink/60">No vehicles currently online on this route.</p>
          ) : (
            <div className="space-y-1 border-t border-dashed border-ink/30 pt-2">
              <p className="mb-1 text-xs text-ink/60">
                {vehicles.length} vehicle{vehicles.length === 1 ? "" : "s"} online on this route right now:
              </p>
              {vehicles.map((v) => (
                <div key={v.vehicle_id} className="flex items-center justify-between text-xs">
                  <span className="text-ink/70">Vehicle {v.vehicle_id.slice(0, 8)}</span>
                  <span className="font-bold text-ink">{v.seats_available} seats free</span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Request-a-stop — the commuter side of Stage 6's driver alert. */}
        {pickupStops.length > 0 && (
          <div className="board relative mt-4 px-4 pb-4 pt-6">
            <span className="tape right-8 rotate-[4deg] bg-marigold/70" />
            <p className="board-heading mb-2">Request a pickup</p>
            <p className="mb-3 text-xs text-ink/60">
              Alerts the nearest approaching driver on this route to stop for you.
            </p>
            <div className="flex gap-2">
              <select
                value={stopRequestStopId || pickupStops[0]?.id || ""}
                onChange={(e) => setStopRequestStopId(e.target.value)}
                className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-2.5 text-sm font-bold text-ink outline-none focus:border-transit"
              >
                {pickupStops.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
              <button
                onClick={() => void requestPickup(stopRequestStopId || pickupStops[0]?.id || "")}
                disabled={stopRequestBusy}
                className="shrink-0 rounded-sm border-2 border-ink bg-marigold px-4 py-2.5 font-display text-xs font-black uppercase text-ink shadow-tape transition active:translate-y-px disabled:opacity-50"
              >
                {stopRequestBusy ? "…" : "Request"}
              </button>
            </div>
            {stopRequestResult && (
              <p className={`mt-2 text-sm font-medium ${stopRequestResult.available ? "text-transit" : "text-flag"}`}>
                {stopRequestResult.message}
              </p>
            )}
          </div>
        )}
      </div>
    );
  }

  // ---- Trip selection ----
  return (
    <div className="mx-auto max-w-md px-4 pb-20 pt-6">
      <div className="mb-3 flex items-center justify-between">
        <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Board</p>
      </div>

      <div className="board relative px-4 pb-4 pt-6">
        <span className="tape left-6" />
        <span className="tape right-8 rotate-[5deg] bg-transit/70" />

        <p className="board-heading mb-3">Choose your trip</p>
        {error && <p className="mb-2 text-sm font-medium text-flag">{error}</p>}

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">Route</label>
            <select
              value={routeId}
              onChange={(e) => selectRoute(e.target.value)}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-sm font-bold text-ink outline-none focus:border-transit"
            >
              <option value="" disabled>
                {loading ? "Loading routes…" : "Select a route…"}
              </option>
              {liveRoutes.map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-[11px] text-ink/50">
              Only live routes with real vehicles are listed here — browse-only network coverage routes aren&rsquo;t
              boardable.
            </p>
          </div>

          <div>
            <label className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">From</label>
            <select
              value={fromStopId}
              onChange={(e) => {
                setFromStopId(e.target.value);
                setToStopId("");
              }}
              disabled={!routeId}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-sm font-bold text-ink outline-none focus:border-transit disabled:opacity-50"
            >
              <option value="" disabled>
                {routeId ? "Select origin stop…" : "Pick a route first"}
              </option>
              {stops.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">To</label>
            <select
              value={toStopId}
              onChange={(e) => setToStopId(e.target.value)}
              disabled={fromIndex < 0}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-sm font-bold text-ink outline-none focus:border-transit disabled:opacity-50"
            >
              <option value="" disabled>
                {fromIndex < 0 ? "Pick an origin first" : "Select destination stop…"}
              </option>
              {stops.map((s, i) => (
                <option key={s.id} value={s.id} disabled={i <= fromIndex}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>

          {issueError && (
            <div className="rounded-sm border-2 border-flag bg-flag/10 px-3 py-2 text-sm font-medium text-flag">
              {issueError}
            </div>
          )}

          <button
            onClick={() => void generatePass()}
            disabled={!routeId || !fromStopId || !toStopId || issuing}
            className="btn-marigold"
          >
            {issuing ? "Generating…" : "Generate boarding pass"}
          </button>

          <p className="text-center text-xs text-ink/50">
            A pass prices the ride on the route you pick — the fare must run in the route&rsquo;s normal stop order.
          </p>
        </div>
      </div>
    </div>
  );
}
