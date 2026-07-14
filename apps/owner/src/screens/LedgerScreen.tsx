import { useEffect, useState } from "react";
import { api, ApiError, type DateRangeParams } from "../api/client";
import { formatDateTime, formatRand } from "../format";
import type { LedgerEntry, LedgerPage } from "../types";

const PAGE_SIZE = 50;

const ENTRY_LABELS: Record<string, string> = {
  fare: "Fare",
  fuel_allocation: "Fuel allocation",
  fuel_authorization: "Fuel authorization",
};

function entryDetail(entry: LedgerEntry): string {
  const d = entry.detail ?? {};
  switch (entry.entry_type) {
    case "fare":
      return `Fare ${formatRand(Number(d.fare_cents ?? 0))} — split ${formatRand(
        Number(d.owner_cents ?? 0),
      )} owner / ${formatRand(Number(d.driver_cents ?? 0))} driver / ${formatRand(
        Number(d.platform_cents ?? 0),
      )} platform`;
    case "fuel_allocation":
      return `Withheld ${d.withhold_pct ?? "?"}% of ${formatRand(Number(d.revenue_balance ?? 0))} revenue balance`;
    case "fuel_authorization":
      return `Status: ${d.status ?? "unknown"}${d.litres ? ` — ${d.litres} litres` : ""}`;
    default:
      return JSON.stringify(d);
  }
}

export function LedgerScreen({ range }: { range: DateRangeParams }) {
  const [page, setPage] = useState<LedgerPage | null>(null);
  const [offset, setOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Reset to the first page whenever the date range changes underneath us.
  useEffect(() => {
    setOffset(0);
  }, [range.from, range.to]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getLedger({ ...range, limit: PAGE_SIZE, offset })
      .then((res) => {
        // The backend serializes a Go nil slice as JSON `null`, not `[]`,
        // when a range has zero ledger entries (see docs/PROGRESS.md's
        // Stage 8 scoping test) — normalize here so the rest of this
        // component can assume `entries` is always an array.
        if (!cancelled) setPage({ ...res, entries: res.entries ?? [] });
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "Failed to load the ledger.");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [range.from, range.to, offset]);

  return (
    <div className="space-y-4">
      {error && (
        <div className="rounded-sm border-2 border-alert bg-alert/10 px-3 py-2 text-sm font-medium text-alert">
          {error}
        </div>
      )}

      {loading && !page && <p className="text-sm text-ink/50">Loading ledger…</p>}

      {page && (
        <>
          <p className="text-xs text-ink/50">
            Every fare crediting your revenue account, every fuel withholding, and every fuel-pump authorization
            against your fleet — the full auditable trail behind the figures on Overview and Fleet.
          </p>

          <div className="ledger-card overflow-x-auto p-2">
            {page.entries.length === 0 ? (
              <p className="p-4 text-sm text-ink/50">No ledger activity in this range.</p>
            ) : (
              <table className="ledger-table">
                <thead>
                  <tr>
                    <th>When</th>
                    <th>Type</th>
                    <th>Amount</th>
                    <th>Detail</th>
                  </tr>
                </thead>
                <tbody>
                  {page.entries.map((e) => (
                    <tr key={`${e.entry_type}-${e.id}`}>
                      <td className="whitespace-nowrap text-xs">{formatDateTime(e.occurred_at)}</td>
                      <td>
                        <span className="rounded-sm border border-ink/20 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-ink/70">
                          {ENTRY_LABELS[e.entry_type] ?? e.entry_type}
                        </span>
                      </td>
                      <td className="num font-bold">{formatRand(e.amount_cents)}</td>
                      <td className="text-xs text-ink/70">{entryDetail(e)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>

          <div className="flex items-center justify-between text-sm">
            <p className="text-ink/50">
              Showing {page.entries.length === 0 ? 0 : offset + 1}–{offset + page.entries.length} of{" "}
              {page.total.toLocaleString()}
            </p>
            <div className="flex gap-2">
              <button
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
                disabled={offset === 0 || loading}
                className="btn-ghost !px-3 !py-1.5 text-xs"
              >
                ← Previous
              </button>
              <button
                onClick={() => setOffset((o) => o + PAGE_SIZE)}
                disabled={offset + page.entries.length >= page.total || loading}
                className="btn-ghost !px-3 !py-1.5 text-xs"
              >
                Next →
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
