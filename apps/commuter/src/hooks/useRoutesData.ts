import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { Route, RouteDetail, Stop } from "../types";

interface RoutesData {
  routes: Route[];
  stops: Stop[];
  loading: boolean;
  error: string | null;
  /** True once the catalogue import has been loaded (any stop tagged
   * source: "catalogue") — derived from the already-fetched stop list, no
   * extra request. Screens use this to degrade coverage/grouping features
   * gracefully rather than erroring when the catalogue isn't imported. */
  catalogueLoaded: boolean;
  /** Fetches (and caches) one route's ordered legs on demand. Deliberately
   * NOT prefetched for every route up front — with the catalogue loaded
   * GET /routes can return 1400+ rows, and fetching each one's detail
   * eagerly (as this hook originally did, back when it also used this as
   * its only way to build a stop list) would mean over a thousand requests
   * before the app was usable. A route's detail is now only ever fetched
   * when a screen actually needs it (a tap in Routes/Board), then reused
   * from this cache. */
  getRouteDetail: (routeId: string) => Promise<RouteDetail | null>;
}

export function useRoutesData(): RoutesData {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [stops, setStops] = useState<Stop[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const detailCache = useRef<Map<string, RouteDetail>>(new Map());
  const inflight = useRef<Map<string, Promise<RouteDetail | null>>>(new Map());

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const [routeList, stopList] = await Promise.all([api.listRoutes(), api.getStops()]);
        if (cancelled) return;
        setRoutes(routeList);
        setStops(stopList);
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

  const getRouteDetail = useCallback(async (routeId: string): Promise<RouteDetail | null> => {
    const cached = detailCache.current.get(routeId);
    if (cached) return cached;

    const pending = inflight.current.get(routeId);
    if (pending) return pending;

    const promise = api
      .getRoute(routeId)
      .then((detail) => {
        detailCache.current.set(routeId, detail);
        return detail;
      })
      .catch(() => null)
      .finally(() => {
        inflight.current.delete(routeId);
      });

    inflight.current.set(routeId, promise);
    return promise;
  }, []);

  const catalogueLoaded = stops.some((s) => s.source === "catalogue");

  return { routes, stops, loading, error, catalogueLoaded, getRouteDetail };
}
