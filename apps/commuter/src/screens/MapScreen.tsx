import { useMemo } from "react";
import { MapContainer, TileLayer, Marker, Popup } from "react-leaflet";
import L from "leaflet";
import type { Route } from "../types";
import type { LiveStatus } from "../hooks/useRouteVehicles";
import { useRouteVehicles } from "../hooks/useRouteVehicles";
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
}

export function MapScreen({ routes, routesError, selectedRouteId, onSelectRoute, onLogout }: MapScreenProps) {
  const { status, vehicles } = useRouteVehicles(selectedRouteId);
  const live = liveTone(status);
  const selectedRoute = routes.find((r) => r.id === selectedRouteId);

  const center = useMemo<[number, number]>(() => {
    if (vehicles.length > 0) return [vehicles[0].lat, vehicles[0].lng];
    return CAPE_TOWN_CENTER;
  }, [vehicles]);

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
              {routes.length === 0 ? "Loading routes…" : "Select a route…"}
            </option>
            {routes.map((r) => (
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
        </div>
      </div>

      <div className="relative mx-auto h-[60vh] w-full max-w-md overflow-hidden rounded-none border-y-2 border-ink/20 sm:h-[65vh] sm:rounded-sm sm:border-2">
        {!selectedRouteId ? (
          <div className="flex h-full items-center justify-center bg-board px-6 text-center">
            <p className="board-heading">Pick a route above to watch its live vehicles.</p>
          </div>
        ) : status === "open" && vehicles.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 bg-board px-6 text-center">
            <p className="board-value">No vehicles right now</p>
            <p className="text-sm text-ink/60">
              No driver is currently online on {selectedRoute?.name ?? "this route"}. Check back shortly.
            </p>
          </div>
        ) : (
          <MapContainer center={center} zoom={DEFAULT_ZOOM} className="h-full w-full" scrollWheelZoom>
            <TileLayer
              attribution='&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
              url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
            />
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
        )}
      </div>

      <p className="mx-auto mt-2 w-full max-w-md px-4 text-xs text-dawn-400">
        Map tiles load from OpenStreetMap over the internet — the one online dependency in this app. No connection,
        no map.
      </p>
    </div>
  );
}
