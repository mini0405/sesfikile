import { useState } from "react";
import type { DateRangeParams } from "../api/client";

export type Preset = "today" | "7d" | "30d" | "custom";

function isoDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/** Approximate preset date math done in the browser's local calendar — the
 * backend is the authority on the actual boundary (it anchors "today" to a
 * fixed Africa/Johannesburg timezone, see internal/analytics/daterange.go),
 * so this is only used to pick a `from=` value to send, never displayed as
 * fact. The screens showing results always echo back the `from`/`to` the
 * API response itself carries. */
function presetFrom(preset: Preset): string | undefined {
  if (preset === "today" || preset === "custom") return undefined;
  const days = preset === "7d" ? 6 : 29;
  const d = new Date();
  d.setDate(d.getDate() - days);
  return isoDate(d);
}

interface DateRangePickerProps {
  onChange: (range: DateRangeParams) => void;
}

const PRESET_LABELS: { id: Preset; label: string }[] = [
  { id: "today", label: "Today" },
  { id: "7d", label: "Last 7 days" },
  { id: "30d", label: "Last 30 days" },
  { id: "custom", label: "Custom" },
];

export function DateRangePicker({ onChange }: DateRangePickerProps) {
  const [preset, setPreset] = useState<Preset>("today");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");

  function applyPreset(next: Preset) {
    setPreset(next);
    if (next === "custom") return;
    onChange({ from: presetFrom(next), to: undefined });
  }

  function applyCustom() {
    onChange({ from: customFrom || undefined, to: customTo || undefined });
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      {PRESET_LABELS.map((p) => (
        <button
          key={p.id}
          onClick={() => applyPreset(p.id)}
          className={`range-btn ${preset === p.id ? "range-btn-active" : ""}`}
        >
          {p.label}
        </button>
      ))}
      {preset === "custom" && (
        <div className="flex items-center gap-2">
          <input
            type="date"
            value={customFrom}
            onChange={(e) => setCustomFrom(e.target.value)}
            className="input-field !py-1.5 text-xs"
          />
          <span className="text-ink/40">to</span>
          <input
            type="date"
            value={customTo}
            onChange={(e) => setCustomTo(e.target.value)}
            className="input-field !py-1.5 text-xs"
          />
          <button onClick={applyCustom} className="btn-brass !px-3 !py-1.5 text-xs">
            Apply
          </button>
        </div>
      )}
    </div>
  );
}
