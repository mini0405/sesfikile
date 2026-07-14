import { formatDateTime } from "../format";

/** Echoes back the exact `from`/`to` the API response carries — never the
 * picker's own local guess — so the displayed range can never disagree with
 * what the figures below it were actually computed over. */
export function ActiveRangeNote({ from, to }: { from: string; to: string }) {
  return (
    <p className="text-xs text-ink/50">
      Showing <span className="font-bold text-ink/70">{formatDateTime(from)}</span> to{" "}
      <span className="font-bold text-ink/70">{formatDateTime(to)}</span>{" "}
      <span className="text-ink/40">(Africa/Johannesburg time, per the backend's date-range boundary)</span>
    </p>
  );
}
