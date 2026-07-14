import { api, type DateRangeParams } from "../api/client";
import { useRangeData } from "../hooks/useRangeData";
import { StatCard } from "../components/StatCard";
import { ActiveRangeNote } from "../components/ActiveRangeNote";
import { formatRand } from "../format";

export function OverviewScreen({ range }: { range: DateRangeParams }) {
  const { data, loading, error } = useRangeData(api.getSummary, range);

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
          {error}
        </div>
      )}

      {loading && !data && <p className="text-sm text-ink/50">Loading summary…</p>}

      {data && (
        <>
          <ActiveRangeNote from={data.from} to={data.to} />

          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatCard label="Revenue" value={formatRand(data.revenue_cents)} tone="brass" />
            <StatCard label="Trips" value={data.trips.toLocaleString()} />
            <StatCard label="Passenger volume" value={data.passenger_volume.toLocaleString()} />
            <StatCard label="Platform fees" value={formatRand(data.platform_fees_cents)} />
            <StatCard label="Driver earnings paid" value={formatRand(data.driver_earnings_cents)} />
            <StatCard label="Fuel account balance" value={formatRand(data.fuel_balance_cents)} tone="signal" />
            <StatCard
              label="Fuel allocated (range)"
              value={formatRand(data.fuel_allocated_cents)}
              sub="Withheld from revenue into the fuel account — Stage 7"
            />
          </div>

          <p className="text-xs text-ink/40">
            Every figure above is read straight from <code className="font-mono">GET /owner/summary</code> —
            revenue/fees/earnings/fuel-allocated are live sums over ledger postings, trips/passenger volume are
            counts of real fare transactions. Nothing here is recomputed or adjusted client-side.
          </p>
        </>
      )}
    </div>
  );
}
