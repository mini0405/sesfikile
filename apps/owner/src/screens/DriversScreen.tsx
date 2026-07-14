import { api, type DateRangeParams } from "../api/client";
import { useRangeData } from "../hooks/useRangeData";
import { ActiveRangeNote } from "../components/ActiveRangeNote";
import { formatRand } from "../format";

export function DriversScreen({ range }: { range: DateRangeParams }) {
  const { data, loading, error } = useRangeData(api.getDrivers, range);

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
          {error}
        </div>
      )}

      {loading && !data && <p className="text-sm text-ink/50">Loading drivers…</p>}

      {data && (
        <>
          <ActiveRangeNote from={data.from} to={data.to} />

          <div className="ledger-card overflow-x-auto p-2">
            {data.drivers.length === 0 ? (
              <p className="p-4 text-sm text-ink/50">No drivers registered to this owner yet.</p>
            ) : (
              <table className="ledger-table">
                <thead>
                  <tr>
                    <th>Driver</th>
                    <th>Assigned vehicle</th>
                    <th>Status</th>
                    <th>Trips (range)</th>
                    <th>Earnings (range)</th>
                  </tr>
                </thead>
                <tbody>
                  {data.drivers.map((d) => (
                    <tr key={d.driver_id}>
                      <td className="font-bold text-ink">{d.full_name}</td>
                      <td>{d.assigned_vehicle_registration ?? <span className="text-ink/40">Unassigned</span>}</td>
                      <td>
                        <span className="flex items-center gap-1.5">
                          <span className={`status-dot ${d.online ? "status-dot-on" : "status-dot-off"}`} />
                          {d.online ? "Online" : <span className="text-ink/50">Offline</span>}
                        </span>
                      </td>
                      <td className="num">{d.trips.toLocaleString()}</td>
                      <td className="num font-bold">{formatRand(d.earnings_cents)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          <p className="text-xs text-ink/40">
            Online status reflects Stage 4's current in-memory telemetry — right now, not a historical log.
            Trips/earnings for the range come straight from <code className="font-mono">GET /owner/drivers</code>.
          </p>
        </>
      )}
    </div>
  );
}
