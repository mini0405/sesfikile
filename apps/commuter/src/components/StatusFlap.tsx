// A split-flap departures tile (see .flap in index.css) rather than a soft
// SaaS status pill — the commuter-side equivalent of the driver app's
// dashboard "led" indicator, but reading like a board at the rank flipping
// to a new status rather than an instrument light on a dash.
interface StatusFlapProps {
  label: string;
  tone: "live" | "warn" | "off" | "alert";
}

const TONE_CLASSES: Record<StatusFlapProps["tone"], string> = {
  live: "flap-live",
  warn: "flap-warn",
  off: "flap-off",
  alert: "flap-alert",
};

export function StatusFlap({ label, tone }: StatusFlapProps) {
  return (
    <span className={`flap ${TONE_CLASSES[tone]}`}>
      <span className="flap-dot" />
      {label}
    </span>
  );
}
