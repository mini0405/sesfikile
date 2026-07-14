import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { Route, RouteDetail, Stop } from "../types";

interface RoutesData {
  routes: Route[];
  stops: Stop[];
  routeDetails: Map<string, RouteDetail>;
  loading: boolean;
  error: string | null;
}

/**
 * There is no GET /stops endpoint (Stage 3 only ever needed from/to by id or
 * name) — so the commuter app derives the full stop list itself by fetching
 * every route's detail once and de-duplicating the stops named in its legs.
 * This is a frontend-only stage; adding a dedicated backend endpoint for this
 * was avoided in favour of reusing what Stage 3 already exposes.
 */
export function useRoutesData(): RoutesData {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [routeDetails, setRouteDetails] = useState<Map<string, RouteDetail>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const routeList = await api.listRoutes();
        if (cancelled) return;
        setRoutes(routeList);

        const details = await Promise.all(routeList.map((r) => api.getRoute(r.id)));
        if (cancelled) return;

        const detailMap = new Map<string, RouteDetail>();
        details.forEach((d, i) => detailMap.set(routeList[i].id, d));
        setRouteDetails(detailMap);
      } catch {
        if (!cancelled) setError("Could not load routes and stops.");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => {
      cancelled = true;
    };
  }, []);

  const stopsById = new Map<string, Stop>();
  for (const detail of routeDetails.values()) {
    for (const leg of detail.legs) {
      stopsById.set(leg.from_stop_id, { id: leg.from_stop_id, name: leg.from_stop_name });
      stopsById.set(leg.to_stop_id, { id: leg.to_stop_id, name: leg.to_stop_name });
    }
  }
  const stops = Array.from(stopsById.values()).sort((a, b) => a.name.localeCompare(b.name));

  return { routes, stops, routeDetails, loading, error };
}
