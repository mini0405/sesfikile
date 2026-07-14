import type { Route } from "../types";
import type { SocketStatus } from "../hooks/useDriverSocket";
import type { GeoStatus, Position } from "../hooks/useGeolocation";
import { StatusPill } from "../components/StatusPill";

interface DashboardScreenProps {
  routes: Route[];
  routesError: string | null;
  selectedRouteId: string | null;
  wantOnline: boolean;
  socketStatus: SocketStatus;
  geoStatus: GeoStatus;
  lastPosition: Position | null;
  onGoOnline: (routeId: string) => void;
  onGoOffline: () => void;
  onLogout: () => void;
}

function socketTone(status: SocketStatus): "green" | "amber" | "red" | "slate" {
  if (status === "open") return "green";
  if (status === "connecting" || status === "reconnecting") return "amber";
  return "slate";
}

function socketLabel(status: SocketStatus): string {
  switch (status) {
    case "open":
      return "Radio up";
    case "connecting":
      return "Connecting";
    case "reconnecting":
      return "Reconnecting";
    default:
      return "Off duty";
  }
}

function geoMessage(status: GeoStatus): { text: string; tone: "green" | "amber" | "red" | "slate" } {
  switch (status) {
    case "watching":
      return { text: "GPS locked", tone: "green" };
    case "denied":
      return { text: "Location blocked — check permissions", tone: "red" };
    case "unsupported":
      return { text: "No GPS on this browser", tone: "red" };
    case "error":
      return { text: "GPS searching…", tone: "amber" };
    default:
      return { text: "GPS idle", tone: "slate" };
  }
}

export function DashboardScreen(props: DashboardScreenProps) {
  const {
    routes,
    routesError,
    selectedRouteId,
    wantOnline,
    socketStatus,
    geoStatus,
    lastPosition,
    onGoOnline,
    onGoOffline,
    onLogout,
  } = props;

  const geo = geoMessage(geoStatus);
  const activeRoute = routes.find((r) => r.id === selectedRouteId);

  return (
    <div className="mx-auto max-w-md px-4 pt-6">
      <div className="mb-5 flex items-center justify-between">
        <p className="board-heading text-tar-400">Ses&rsquo;fikile · Driver</p>
        <button onClick={onLogout} className="text-xs font-bold uppercase tracking-wide text-tar-400 active:text-rank">
          Log out
        </button>
      </div>

      {/* The windscreen destination board — the app's one signature object. */}
      <div className="board relative mb-4 px-5 pb-5 pt-7">
        <span className="tape left-6" />
        <span className="tape right-8 rotate-[5deg] bg-taxi/70" />

        {!wantOnline ? (
          <>
            <p className="board-heading mb-3">Choose today&rsquo;s route</p>
            {routesError && <p className="mb-2 text-sm font-medium text-brake">{routesError}</p>}
            <select
              id="route"
              defaultValue=""
              onChange={(e) => {
                if (e.target.value) onGoOnline(e.target.value);
              }}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 font-display text-sm font-black uppercase tracking-wide text-ink outline-none focus:border-taxi"
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
            <p className="mt-3 text-xs leading-relaxed text-ink/60">
              Selecting a route hangs your board and starts your GPS straight away — there&rsquo;s no separate
              &ldquo;confirm&rdquo; step.
            </p>
          </>
        ) : (
          <>
            <p className="board-heading mb-1">Now boarding</p>
            <p className="board-value mb-4 break-words">{activeRoute?.name ?? "…"}</p>

            {lastPosition && (
              <p className="mb-2 font-mono text-xs tracking-tight text-ink/50">
                {lastPosition.lat.toFixed(5)}, {lastPosition.lng.toFixed(5)}
              </p>
            )}
          </>
        )}
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-2">
        <StatusPill label={socketLabel(socketStatus)} tone={socketTone(socketStatus)} />
        {wantOnline && <StatusPill label={geo.text} tone={geo.tone} />}
      </div>

      {wantOnline && (
        <button onClick={onGoOffline} className="btn-brake">
          End shift — go offline
        </button>
      )}
    </div>
  );
}
