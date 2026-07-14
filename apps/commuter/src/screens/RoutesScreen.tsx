import type { Route, RouteDetail } from "../types";

interface RoutesScreenProps {
  routes: Route[];
  routeDetails: Map<string, RouteDetail>;
  loading: boolean;
  error: string | null;
  selectedRouteId: string | null;
  onSelectRoute: (routeId: string | null) => void;
}

export function RoutesScreen({ routes, routeDetails, loading, error, selectedRouteId, onSelectRoute }: RoutesScreenProps) {
  const detail = selectedRouteId ? routeDetails.get(selectedRouteId) : null;

  if (detail) {
    return (
      <div className="mx-auto max-w-md px-4 pb-20 pt-6">
        <button
          onClick={() => onSelectRoute(null)}
          className="mb-3 text-xs font-bold uppercase tracking-wide text-dawn-400 active:text-marigold-700"
        >
          ← All routes
        </button>

        <div className="board relative px-4 pb-4 pt-6">
          <span className="tape left-6" />
          <span className="tape right-8 rotate-[5deg] bg-marigold/70" />

          <p className="board-heading mb-1">{detail.route.association_name}</p>
          <h2 className="board-value mb-4 break-words">{detail.route.name}</h2>

          <div className="space-y-1 border-y border-dashed border-ink/30 py-3">
            {detail.legs.length === 0 ? (
              <p className="text-sm text-ink/60">No stops recorded for this route.</p>
            ) : (
              <>
                <div className="flex items-center gap-2 text-sm">
                  <span className="h-2 w-2 shrink-0 rounded-full bg-transit" />
                  <span className="font-bold text-ink">{detail.legs[0].from_stop_name}</span>
                </div>
                {detail.legs.map((leg, i) => (
                  <div key={leg.id} className="flex items-center gap-2 text-sm">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        i === detail.legs.length - 1 ? "bg-transit" : "bg-ink/30"
                      }`}
                    />
                    <span className={i === detail.legs.length - 1 ? "font-bold text-ink" : "text-ink/70"}>
                      {leg.to_stop_name}
                    </span>
                    <span className="ml-auto text-xs text-ink/50">R{(leg.fare_cents / 100).toFixed(2)}</span>
                  </div>
                ))}
              </>
            )}
          </div>

          {detail.legs.length > 0 && (
            <p className="mt-3 text-xs text-ink/60">
              Full-route fare: R{(detail.legs.reduce((sum, l) => sum + l.fare_cents, 0) / 100).toFixed(2)}
            </p>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-md px-4 pb-20 pt-6">
      <div className="mb-3 flex items-center justify-between">
        <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Routes</p>
      </div>

      {error && <p className="mb-3 text-sm font-medium text-flag">{error}</p>}
      {loading && routes.length === 0 && <p className="text-sm text-ink/60">Loading routes…</p>}

      <div className="space-y-2">
        {routes.map((r) => (
          <button
            key={r.id}
            onClick={() => onSelectRoute(r.id)}
            className="board flex w-full items-center justify-between px-4 py-3 text-left transition active:translate-y-px"
          >
            <span>
              <span className="block font-display text-sm font-black uppercase tracking-wide text-ink">{r.name}</span>
              <span className="block text-xs text-ink/60">{r.association_name}</span>
            </span>
            <span className="text-ink/40">→</span>
          </button>
        ))}
      </div>
    </div>
  );
}
