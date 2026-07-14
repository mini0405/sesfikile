import { useState } from "react";
import { api, ApiError } from "../api/client";
import type { RouteSearchResult, Stop } from "../types";

interface SearchScreenProps {
  stops: Stop[];
  stopsLoading: boolean;
  stopsError: string | null;
  onViewRoute: (routeId: string) => void;
}

function orderedStopNames(result: RouteSearchResult): string[] {
  const names: string[] = [];
  result.segments.forEach((seg, i) => {
    seg.legs.forEach((leg, j) => {
      if (i === 0 && j === 0) names.push(leg.from_stop_name);
      names.push(leg.to_stop_name);
    });
  });
  return names;
}

export function SearchScreen({ stops, stopsLoading, stopsError, onViewRoute }: SearchScreenProps) {
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [result, setResult] = useState<RouteSearchResult | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  async function handleSearch() {
    if (!from || !to) return;
    setLoading(true);
    setError(null);
    setNotFound(false);
    setResult(null);
    setSearched(true);
    try {
      const res = await api.searchRoutes(from, to);
      setResult(res);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        setNotFound(true);
      } else {
        setError(err instanceof ApiError ? err.message : "Search failed. Try again.");
      }
    } finally {
      setLoading(false);
    }
  }

  const fromStop = stops.find((s) => s.id === from);
  const toStop = stops.find((s) => s.id === to);
  const stopNames = result ? orderedStopNames(result) : [];

  return (
    <div className="mx-auto max-w-md px-4 pb-20 pt-6">
      <div className="mb-3 flex items-center justify-between">
        <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Plan a trip</p>
      </div>

      <div className="board relative mb-4 px-4 pb-4 pt-6">
        <span className="tape left-6" />
        <span className="tape right-8 rotate-[5deg] bg-transit/70" />

        <p className="board-heading mb-3">Where are you headed?</p>

        {stopsError && <p className="mb-2 text-sm font-medium text-flag">{stopsError}</p>}

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">From</label>
            <select
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-sm font-bold text-ink outline-none focus:border-transit"
            >
              <option value="" disabled>
                {stopsLoading ? "Loading stops…" : "Select origin…"}
              </option>
              {stops.map((s) => (
                <option key={s.id} value={s.id} disabled={s.id === to}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="mb-1 block text-xs font-bold uppercase tracking-wide text-ink/60">To</label>
            <select
              value={to}
              onChange={(e) => setTo(e.target.value)}
              className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-3 text-sm font-bold text-ink outline-none focus:border-transit"
            >
              <option value="" disabled>
                {stopsLoading ? "Loading stops…" : "Select destination…"}
              </option>
              {stops.map((s) => (
                <option key={s.id} value={s.id} disabled={s.id === from}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>

          <button onClick={handleSearch} disabled={!from || !to || loading} className="btn-marigold">
            {loading ? "Searching…" : "Find my ride"}
          </button>
        </div>
      </div>

      {error && (
        <div className="mb-4 rounded-sm border-2 border-flag bg-flag/10 px-3 py-2 text-sm font-medium text-flag">
          {error}
        </div>
      )}

      {searched && notFound && (
        <div className="ticket text-center">
          <p className="board-value mb-2">No route found</p>
          <p className="text-sm text-ink/70">
            No direct or one-transfer taxi connects {fromStop?.name ?? "this origin"} to{" "}
            {toStop?.name ?? "this destination"} yet.
          </p>
        </div>
      )}

      {result && (
        <div className="ticket">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <p className="board-heading mb-1">{result.transfers === 0 ? "Direct" : `${result.transfers} transfer`}</p>
              <p className="text-sm font-bold">
                {fromStop?.name} → {toStop?.name}
              </p>
            </div>
            <div className="text-right">
              <p className="board-heading mb-1">Total fare</p>
              <p className="font-display text-2xl font-black">R{(result.total_fare_cents / 100).toFixed(2)}</p>
            </div>
          </div>

          <div className="mb-4 space-y-1 border-y border-dashed border-ink/30 py-3">
            {stopNames.map((name, i) => (
              <div key={i} className="flex items-center gap-2 text-sm">
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    i === 0 || i === stopNames.length - 1 ? "bg-transit" : "bg-ink/30"
                  }`}
                />
                <span className={i === 0 || i === stopNames.length - 1 ? "font-bold text-ink" : "text-ink/70"}>
                  {name}
                </span>
              </div>
            ))}
          </div>

          <div className="space-y-2">
            {result.segments.map((seg, i) => (
              <div key={seg.route_id}>
                {i > 0 && (
                  <p className="my-2 text-center text-[11px] font-bold uppercase tracking-wide text-marigold-700">
                    ⟳ Transfer at {seg.legs[0]?.from_stop_name}
                  </p>
                )}
                <button
                  onClick={() => onViewRoute(seg.route_id)}
                  className="flex w-full items-center justify-between rounded-sm border-2 border-ink/20 bg-board-dim px-3 py-2 text-left transition active:translate-y-px"
                >
                  <span className="text-sm font-bold text-ink">{seg.route_name}</span>
                  <span className="text-xs font-bold text-ink/60">R{(seg.fare_cents / 100).toFixed(2)}</span>
                </button>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
