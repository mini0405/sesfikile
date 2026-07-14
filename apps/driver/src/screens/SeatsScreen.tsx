import { useCallback, useEffect, useState } from "react";
import { api, ApiError } from "../api/client";
import type { SeatsResponse } from "../types";

interface SeatsScreenProps {
  online: boolean;
  seats: SeatsResponse | null;
  seatsError: string | null;
  onAdjust: (next: { delta?: number; seats_available?: number }) => Promise<void>;
}

// A seat grid — one square per physical seat in the vehicle, filled
// (rank-yellow) for available, dim for taken. Reads at a glance the way a
// conductor counts seats, not just an abstract "12/16" figure.
function SeatGrid({ available, total }: { available: number; total: number }) {
  return (
    <div className="mt-1 flex flex-wrap gap-2">
      {Array.from({ length: total }, (_, i) => (
        <span
          key={i}
          className={`h-6 w-6 rounded-sm border-2 ${
            i < available ? "border-rank bg-rank" : "border-tar-600 bg-tar-800"
          }`}
        />
      ))}
    </div>
  );
}

export function SeatsScreen({ online, seats, seatsError, onAdjust }: SeatsScreenProps) {
  const [balanceCents, setBalanceCents] = useState<number | null>(null);
  const [balanceError, setBalanceError] = useState<string | null>(null);
  const [loadingBalance, setLoadingBalance] = useState(false);

  const refreshBalance = useCallback(() => {
    setLoadingBalance(true);
    setBalanceError(null);
    api
      .balance()
      .then((res) => setBalanceCents(res.balance_cents))
      .catch((err) => setBalanceError(err instanceof ApiError ? err.message : "Could not load earnings."))
      .finally(() => setLoadingBalance(false));
  }, []);

  useEffect(() => {
    refreshBalance();
  }, [refreshBalance]);

  return (
    <div className="mx-auto max-w-md px-4 pt-6">
      <p className="board-heading mb-1 text-tar-400">Vehicle</p>
      <h1 className="mb-4 font-display text-2xl font-black uppercase tracking-tight text-board">
        Seats &amp; earnings
      </h1>

      <section className="mb-4 rounded-sm border-2 border-tar-600 bg-tar-800 p-5">
        <p className="board-heading mb-2 text-tar-400">Seats available</p>
        {!online && <p className="text-sm font-medium text-taxi">Go online from the Board tab to manage seats.</p>}
        {online && !seats && !seatsError && <p className="text-sm text-tar-400">Loading…</p>}
        {seatsError && <p className="text-sm font-medium text-brake">{seatsError}</p>}

        {online && seats && (
          <>
            <div className="mb-4 flex items-end gap-2">
              <p className="font-mono text-4xl font-bold tabular-nums text-board">{seats.seats_available}</p>
              <p className="pb-1 font-mono text-lg text-tar-400">/ {seats.seats_total}</p>
            </div>
            <SeatGrid available={seats.seats_available} total={seats.seats_total} />
            <div className="mt-5 flex items-center gap-3">
              <button
                onClick={() => void onAdjust({ delta: -1 })}
                disabled={seats.seats_available <= 0}
                className="flex-1 rounded-sm border-2 border-tar-600 py-3 font-display text-lg font-black text-board disabled:opacity-30"
                aria-label="One fewer seat available"
              >
                −
              </button>
              <button
                onClick={() => void onAdjust({ delta: 1 })}
                disabled={seats.seats_available >= seats.seats_total}
                className="flex-1 rounded-sm border-2 border-rank py-3 font-display text-lg font-black text-rank disabled:opacity-30"
                aria-label="One more seat available"
              >
                +
              </button>
            </div>
          </>
        )}
      </section>

      {/* The cash float — earnings styled like a board readout, not a bank card. */}
      <section className="board relative px-5 pb-5 pt-7">
        <span className="tape left-6 bg-taxi/70" />
        <div className="mb-1 flex items-center justify-between">
          <p className="board-heading">Earnings balance</p>
          <button
            onClick={refreshBalance}
            className="text-[11px] font-bold uppercase tracking-wide text-taxi active:text-taxi-600"
          >
            Refresh
          </button>
        </div>
        {loadingBalance && balanceCents === null && <p className="text-sm text-ink/50">Loading…</p>}
        {balanceError && <p className="text-sm font-medium text-brake">{balanceError}</p>}
        {balanceCents !== null && (
          <p className="font-mono text-4xl font-bold tabular-nums">R{(balanceCents / 100).toFixed(2)}</p>
        )}
        <p className="mt-2 text-xs leading-relaxed text-ink/60">
          Your driver_earnings wallet balance — credited the instant a fare pass is scanned. A per-trip breakdown
          isn&rsquo;t exposed to drivers yet (that detail currently lives in the owner dashboard).
        </p>
      </section>
    </div>
  );
}
