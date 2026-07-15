import { useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { RouteGeometrySummary } from "../types";

export type CoverageStatus = "idle" | "loading" | "loaded" | "error";

/**
 * Lazily fetches every catalogue route's (server-decimated) polyline in one
 * bulk request, only once the "network coverage" layer is actually switched
 * on — not on every app load, since most sessions will never toggle it.
 * Cached for the component's lifetime so toggling off/on again doesn't
 * refetch.
 */
export function useRouteGeometries(enabled: boolean) {
  const [status, setStatus] = useState<CoverageStatus>("idle");
  const [geometries, setGeometries] = useState<RouteGeometrySummary[]>([]);
  const fetchedRef = useRef(false);

  useEffect(() => {
    if (!enabled || fetchedRef.current) return;
    fetchedRef.current = true;
    setStatus("loading");
    api
      .getRouteGeometries()
      .then((rows) => {
        setGeometries(rows);
        setStatus("loaded");
      })
      .catch(() => {
        fetchedRef.current = false;
        setStatus("error");
      });
  }, [enabled]);

  return { status, geometries };
}
