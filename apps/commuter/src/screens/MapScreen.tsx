import { useMemo, useState } from "react";
import { MapContainer, TileLayer, Marker, Polyline, Popup } from "react-leaflet";
import L from "leaflet";
import type { Route } from "../types";
import type { LiveStatus } from "../hooks/useRouteVehicles";
import { useRouteVehicles } from "../hooks/useRouteVehicles";
import { useRouteGeometries } from "../hooks/useRouteGeometries";
import { StatusFlap } from "../components/StatusFlap";

// Cape Town — every seeded corridor (Stage 3) falls inside this view.
const CAPE_TOWN_CENTER: [number, number] = [-33.9715, 18.5];
const DEFAULT_ZOOM = 11;

function vehicleIcon(seatsAvailable: number) {
  return L.divIcon({
    className: "",
    html: `<div class="vehicle-marker">🚐 ${seatsAvailable}</div>`,
    iconSize: undefined,
    iconAnchor: [24, 14],
  });
}

function liveTone(status: LiveStatus): { tone: "live" | "warn" | "off"; label: string } {
  switch (status) {
    case "open":
      return { tone: "live", label: "Live" };
    case "connecting":
      return { tone: "warn", label: "Connecting" };
    case "reconnecting":
      return { tone: "warn", label: "Reconnecting" };
    default:
      return { tone: "off", label: "Not watching" };
  }
}

interface MapScreenProps {
  routes: Route[];
  routesError: string | null;
  selectedRouteId: string | null;
  onSelectRoute: (routeId: string) => void;
  onLogout: () => void;
  /** Derived from GET /stops (any row tagged source: "catalogue") — no
   * separate request needed to know whether the coverage layer has
   * anything to draw. */
  catalogueLoaded: boolean;
}

export function MapScreen({
  routes,
  routesError,
  selectedRouteId,
  onSelectRoute,
  onLogout,
  catalogueLoaded,
}: MapScreenProps) {
  const { status, vehicles } = useRouteVehicles(selectedRouteId);
  const live = liveTone(status);

  // The "watching route" picker only ever lists live/seeded routes — a
  // catalogue route never has a vehicle to watch, so listing all 1447 of
  // them here would be both useless (every one shows "0 vehicles" forever)
  // and, at that size, a genuinely broken dropdown. The separate "network
  // coverage" toggle below is how the catalogue's routes get on the map at
  // all — as a backdrop layer, not something to "watch".
  const liveRoutes = useMemo(() => routes.filter((r) => r.source !== "catalogue"), [routes]);
  const selectedRoute = liveRoutes.find((r) => r.id === selectedRouteId);

  const [coverageOn, setCoverageOn] = useState(false);
  const { status: coverageStatus, geometries } = useRouteGeometries(coverageOn && catalogueLoaded);

  const coveragePositions = useMemo<[number, number][][]>(
    () => geometries.map((g) => g.points.map(([lon, lat]) => [lat, lon] as [number, number])),
    [geometries],
  );

  const center = useMemo<[number, number]>(() => {
    if (vehicles.length > 0) return [vehicles[0].lat, vehicles[0].lng];
    return CAPE_TOWN_CENTER;
  }, [vehicles]);

  const noRouteSelected = !selectedRouteId;
  const noVehiclesOnRoute = status === "open" && vehicles.length === 0;

  // Coverage OFF: identical three-state behaviour to before this stage (pick
  // a route / empty state / live map). Coverage ON: the map surface itself
  // IS the coverage layer, so it stays up regardless of route-watching state
  // — a "no vehicles on this route" notice becomes a small overlay chip
  // instead of blocking the whole screen.
  const showMap = coverageOn || (!noRouteSelected && !noVehiclesOnRoute);

  return (
    <div className="flex min-h-screen flex-col pb-20">
      <div className="mx-auto w-full max-w-md px-4 pt-6">
        <div className="mb-3 flex items-center justify-between">
          <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Live map</p>
          <button onClick={onLogout} className="text-xs font-bold uppercase tracking-wide text-dawn-400 active:text-marigold-700">
            Log out
          </button>
        </div>

        <div className="board relative mb-3 px-4 pb-4 pt-6">
          <span className="tape left-6" />
          <span className="tape right-8 rotate-[5deg] bg-marigold/70" />

          <p className="board-heading mb-2">Watching route</p>
          {routesError && <p className="mb-2 text-sm font-medium text-flag">{routesError}</p>}
          <select
            value={selectedRouteId ?? ""}
            onChange={(e) => onSelectRoute(e.target.value)}
            className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 font-display text-sm font-black uppercase tracking-wide text-ink outline-none focus:border-transit"
          >
            <option value="" disabled>
              {liveRoutes.length === 0 ? "Loading routes…" : "Select a route…"}
            </option>
            {liveRoutes.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
              </option>
            ))}
          </select>

          <div className="mt-3 flex flex-wrap items-center gap-2">
            <StatusFlap label={live.label} tone={live.tone} />
            {status === "open" && (
              <StatusFlap
                label={vehicles.length === 0 ? "0 vehicles" : `${vehicles.length} vehicle${vehicles.length === 1 ? "" : "s"}`}
                tone={vehicles.length === 0 ? "off" : "live"}
              />
            )}
          </div>

          {/* Network coverage toggle — see the legend below the map for what
              it draws. Degrades gracefully (disabled, not hidden/erroring)
              when the catalogue hasn't been imported on this backend. */}
          <div className="mt-3 border-t border-dashed border-ink/20 pt-3">
            <label
              className={`flex items-center justify-between gap-3 ${
                catalogueLoaded ? "cursor-pointer" : "cursor-not-allowed opacity-60"
              }`}
            >
              <span>
                <span className="block font-display text-xs font-black uppercase tracking-wide text-ink">
                  Network coverage
                </span>
                <span className="block text-[11px] text-ink/60">
                  {catalogueLoaded
                    ? "Show the full City of Cape Town taxi route network as a backdrop."
                    : "Not available — this backend has no route catalogue imported."}
                </span>
              </span>
              <input
                type="checkbox"
                checked={coverageOn}
                disabled={!catalogueLoaded}
                onChange={(e) => setCoverageOn(e.target.checked)}
                className="h-5 w-9 shrink-0 accent-ink disabled:cursor-not-allowed"
              />
            </label>
            {coverageOn && coverageStatus === "loading" && (
              <p className="mt-1 text-[11px] text-ink/50">Loading network coverage…</p>
            )}
            {coverageOn && coverageStatus === "error" && (
              <p className="mt-1 text-[11px] text-flag">Could not load network coverage. Try again.</p>
            )}
          </div>
        </div>
      </div>

      <div className="relative mx-auto h-[60vh] w-full max-w-md overflow-hidden rounded-none border-y-2 border-ink/20 sm:h-[65vh] sm:rounded-sm sm:border-2">
        {!showMap ? (
          noRouteSelected ? (
            <div className="flex h-full items-center justify-center bg-board px-6 text-center">
              <p className="board-heading">Pick a route above to watch its live vehicles.</p>
            </div>
          ) : (
            <div className="flex h-full flex-col items-center justify-center gap-3 bg-board px-6 text-center">
              <p className="board-value">No vehicles right now</p>
              <p className="text-sm text-ink/60">
                No driver is currently online on {selectedRoute?.name ?? "this route"}. Check back shortly.
              </p>
            </div>
          )
        ) : (
          <>
            <MapContainer center={center} zoom={DEFAULT_ZOOM} className="h-full w-full" scrollWheelZoom preferCanvas>
              <TileLayer
                attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
                url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
              />
              {/* Backdrop first, in its own render pass — Leaflet's default
                  pane z-indices already put polylines (overlayPane, 400)
                  beneath markers (markerPane, 600), so this can never visually
                  cover a vehicle even before considering draw order. */}
              {coverageOn && coveragePositions.length > 0 && (
                <Polyline
                  positions={coveragePositions}
                  pathOptions={{ color: "#201C16", weight: 1.25, opacity: 0.38, dashArray: "1 6", lineCap: "round" }}
                  interactive={false}
                />
              )}
              {vehicles.map((v) => (
                <Marker key={v.vehicle_id} position={[v.lat, v.lng]} icon={vehicleIcon(v.seats_available)}>
                  <Popup>
                    <p className="font-display text-sm font-black uppercase">{selectedRoute?.name}</p>
                    <p className="text-xs">
                      {v.seats_available} of {v.seats_total} seats free
                    </p>
                  </Popup>
                </Marker>
              ))}
            </MapContainer>

            {coverageOn && noVehiclesOnRoute && (
              <div className="pointer-events-none absolute left-2 top-2 z-[1000] rounded-sm border-2 border-ink/70 bg-board/95 px-2.5 py-1.5 shadow-tape">
                <p className="text-[11px] font-bold text-ink/70">
                  No vehicles online on {selectedRoute?.name ?? "this route"} right now.
                </p>
              </div>
            )}

            {coverageOn && (
              <div className="pointer-events-none absolute bottom-2 left-2 z-[1000] space-y-1 rounded-sm border-2 border-ink bg-board/95 px-3 py-2 shadow-tape">
                <div className="flex items-center gap-2">
                  <span className="flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full border-2 border-ink bg-transit text-[7px]">
                    🚐
                  </span>
                  <span className="text-[10px] font-bold uppercase tracking-wide text-ink">
                    Live routes — vehicles running now
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <span
                    className="h-0 w-5 border-t-[2px] border-dashed"
                    style={{ borderColor: "#201C16", opacity: 0.5 }}
                  />
                  <span className="text-[10px] font-bold uppercase tracking-wide text-ink/60">
                    Network coverage — real City data, no live vehicles, fares estimated
                  </span>
                </div>
              </div>
            )}
          </>
        )}
      </div>

      <p className="mx-auto mt-2 w-full max-w-md px-4 text-xs text-dawn-400">
        Map tiles load from OpenStreetMap over the internet — the one online dependency in this app. No connection,
        no map.
      </p>
    </div>
  );
}
