import { useEffect, useState } from "react";
import { ApiError, type DateRangeParams } from "../api/client";

interface RangeData<T> {
  data: T | null;
  loading: boolean;
  error: string | null;
}

/** Re-fetches whenever `range` changes — every /owner/* screen shares this
 * shape: pass in the fetcher (already scoped to the caller's own JWT
 * server-side), get back {data, loading, error}. */
export function useRangeData<T>(fetcher: (range: DateRangeParams) => Promise<T>, range: DateRangeParams): RangeData<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    fetcher(range)
      .then((res) => {
        if (!cancelled) setData(res);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "Failed to load data.");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range.from, range.to]);

  return { data, loading, error };
}
