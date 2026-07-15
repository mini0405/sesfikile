import { useEffect, useState } from "react";
import type { RouteDetail } from "../types";

/** Loads one route's detail on demand via the given fetcher (normally
 * useRoutesData's cached getRouteDetail), re-running whenever routeId
 * changes and ignoring a stale response if routeId changes again before
 * the previous fetch resolves. */
export function useRouteDetail(
  routeId: string | null,
  getRouteDetail: (routeId: string) => Promise<RouteDetail | null>,
) {
  const [detail, setDetail] = useState<RouteDetail | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!routeId) {
      setDetail(null);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    getRouteDetail(routeId).then((d) => {
      if (cancelled) return;
      setDetail(d);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [routeId]);

  return { detail, loading };
}
