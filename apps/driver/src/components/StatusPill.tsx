// A dashboard indicator light (see .led in index.css) rather than a soft
// SaaS status pill — every connection/GPS/socket state in this app reads
// like a physical instrument-cluster light, not a rounded badge.
interface StatusPillProps {
  label: string;
  tone: "green" | "amber" | "red" | "slate";
}

const TONE_CLASSES: Record<StatusPillProps["tone"], string> = {
  green: "led-on",
  amber: "led-warn",
  red: "led-alert",
  slate: "",
};

export function StatusPill({ label, tone }: StatusPillProps) {
  return (
    <span className={`led ${TONE_CLASSES[tone]}`}>
      <span className="led-dot" />
      {label}
    </span>
  );
}
