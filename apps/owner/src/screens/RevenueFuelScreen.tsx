import {
  Bar,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api, type DateRangeParams } from "../api/client";
import { useRangeData } from "../hooks/useRangeData";
import { StatCard } from "../components/StatCard";
import { ActiveRangeNote } from "../components/ActiveRangeNote";
import { formatDate, formatRand } from "../format";

const REVENUE_COLOR = "#8F6E2E"; // brass
const ALLOCATED_COLOR = "#1B6E74"; // signal
const CONSUMED_COLOR = "#A8402F"; // alert

function randTick(v: number): string {
  // Cents -> a short Rand tick label. No abbreviation beyond 2dp rounding —
  // this is a demo-scale dataset, not one where R1.2k-style shortening is
  // needed, and shortening incorrectly is exactly the kind of "misleading
  // axis" the brief warns against.
  return formatRand(v);
}

export function RevenueFuelScreen({ range }: { range: DateRangeParams }) {
  const { data, loading, error } = useRangeData(api.getRevenueVsFuel, range);

  const ratioPct =
    data && data.revenue_cents > 0 ? ((data.fuel_allocated_cents / data.revenue_cents) * 100).toFixed(1) : null;

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
          {error}
        </div>
      )}

      {loading && !data && <p className="text-sm text-ink/50">Loading revenue vs fuel…</p>}

      {data && (
        <>
          <ActiveRangeNote from={data.from} to={data.to} />

          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            <StatCard label="Revenue (range)" value={formatRand(data.revenue_cents)} tone="brass" />
            <StatCard label="Fuel allocated (range)" value={formatRand(data.fuel_allocated_cents)} tone="signal" />
            <StatCard label="Fuel consumed (range)" value={formatRand(data.fuel_consumed_cents)} />
            <StatCard
              label="Fuel share of revenue"
              value={ratioPct !== null ? `${ratioPct}%` : "—"}
              sub="Fuel allocated ÷ revenue, computed for display only — both source figures are the API's own totals above"
            />
          </div>

          <div className="ledger-card p-5">
            <p className="card-heading mb-4">Revenue vs fuel, by day</p>
            {data.series.length === 0 ? (
              <p className="text-sm text-ink/50">No activity in this range yet.</p>
            ) : (
              <div className="h-80 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <ComposedChart data={data.series} margin={{ top: 8, right: 16, left: 8, bottom: 8 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="rgba(30,27,22,0.1)" />
                    <XAxis
                      dataKey="date"
                      tickFormatter={(d: string) => formatDate(d)}
                      tick={{ fontSize: 11, fill: "#1E1B16" }}
                      stroke="rgba(30,27,22,0.3)"
                    />
                    <YAxis
                      tickFormatter={randTick}
                      domain={[0, "auto"]}
                      tick={{ fontSize: 11, fill: "#1E1B16" }}
                      stroke="rgba(30,27,22,0.3)"
                      width={90}
                    />
                    <Tooltip
                      labelFormatter={(d: string) => formatDate(d)}
                      formatter={(value: number, name: string) => [formatRand(value), name]}
                      contentStyle={{
                        borderRadius: 2,
                        border: "1px solid rgba(30,27,22,0.2)",
                        fontSize: 12,
                      }}
                    />
                    <Legend wrapperStyle={{ fontSize: 12 }} />
                    <Bar dataKey="revenue_cents" name="Revenue" fill={REVENUE_COLOR} radius={[2, 2, 0, 0]} />
                    <Bar
                      dataKey="fuel_allocated_cents"
                      name="Fuel allocated"
                      fill={ALLOCATED_COLOR}
                      radius={[2, 2, 0, 0]}
                    />
                    <Line
                      type="monotone"
                      dataKey="fuel_consumed_cents"
                      name="Fuel consumed"
                      stroke={CONSUMED_COLOR}
                      strokeWidth={2}
                      strokeDasharray="4 3"
                      dot={{ r: 3 }}
                    />
                  </ComposedChart>
                </ResponsiveContainer>
              </div>
            )}
            <p className="mt-3 text-xs text-ink/40">
              Bars/line plotted straight from <code className="font-mono">GET /owner/revenue-vs-fuel</code>'s daily
              series — the Y axis always starts at zero (no truncated axis) and is labelled in Rands, not raw cents.
            </p>
          </div>
        </>
      )}
    </div>
  );
}
