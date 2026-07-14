import { api, type DateRangeParams } from "../api/client";
import { useRangeData } from "../hooks/useRangeData";
import { ActiveRangeNote } from "../components/ActiveRangeNote";
import { formatRand } from "../format";

export function FleetScreen({ range }: { range: DateRangeParams }) {
  const { data, loading, error } = useRangeData(api.getVehicles, range);

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
          {error}
        </div>
      )}

      {loading && !data && <p className="text-sm text-ink/50">Loading fleet…</p>}

      {data && (
        <>
          <ActiveRangeNote from={data.from} to={data.to} />

          <div className="ledger-card overflow-x-auto p-2">
            {data.vehicles.length === 0 ? (
              <p className="p-4 text-sm text-ink/50">No vehicles registered to this owner yet.</p>
            ) : (
              <table className="ledger-table">
                <thead>
                  <tr>
                    <th>Registration</th>
                    <th>Assigned driver</th>
                    <th>Status</th>
                    <th>Seats</th>
                    <th>Trips (range)</th>
                    <th>Revenue (range)</th>
                    <th>Fuel quota</th>
                  </tr>
                </thead>
                <tbody>
                  {data.vehicles.map((v) => (
                    <tr key={v.vehicle_id}>
                      <td className="font-bold text-ink">{v.registration}</td>
                      <td>{v.assigned_driver_name ?? <span className="text-ink/40">Unassigned</span>}</td>
                      <td>
                        <span className="flex items-center gap-1.5">
                          <span className={`status-dot ${v.online ? "status-dot-on" : "status-dot-off"}`} />
                          {v.online ? (
                            <span>
                              Online
                              {v.current_route_name && (
                                <span className="text-ink/50"> · {v.current_route_name}</span>
                              )}
                            </span>
                          ) : (
                            <span className="text-ink/50">Offline</span>
                          )}
                        </span>
                      </td>
                      <td className="num">
                        {v.online && v.seats_available !== undefined ? `${v.seats_available} / ${v.seats_total}` : `— / ${v.seats_total}`}
                      </td>
                      <td className="num">{v.trips.toLocaleString()}</td>
                      <td className="num font-bold">{formatRand(v.revenue_cents)}</td>
                      <td className="num text-xs">
                        {formatRand(v.fuel_available_cents)} available
                        <br />
                        <span className="text-ink/40">of {formatRand(v.fuel_quota_cents)} quota</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          <p className="text-xs text-ink/40">
            Live status/route/seats reflect Stage 4's current in-memory telemetry — right now, not a historical log.
            Trips/revenue for the range and fuel quota figures come straight from{" "}
            <code className="font-mono">GET /owner/vehicles</code>.
          </p>
        </>
      )}
    </div>
  );
}
