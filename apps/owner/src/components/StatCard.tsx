interface StatCardProps {
  label: string;
  value: string;
  sub?: string;
  tone?: "default" | "brass" | "signal";
}

const TONE_CLASSES: Record<NonNullable<StatCardProps["tone"]>, string> = {
  default: "text-ink",
  brass: "text-brass-700",
  signal: "text-signal-600",
};

/** A single headline figure, straight off a Stage 8 /owner/* response — see
 * the "ledger-reconciled" integrity note in README.md. Never recomputed. */
export function StatCard({ label, value, sub, tone = "default" }: StatCardProps) {
  return (
    <div className="ledger-card p-5">
      <p className="card-heading mb-2">{label}</p>
      <p className={`stat-value ${TONE_CLASSES[tone]}`}>{value}</p>
      {sub && <p className="mt-1 text-xs text-ink/50">{sub}</p>}
    </div>
  );
}
