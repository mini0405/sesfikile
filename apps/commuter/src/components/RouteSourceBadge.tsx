import type { RouteSource } from "../types";

interface RouteSourceBadgeProps {
  source: RouteSource;
  /** Smaller inline variant for dense lists (route list rows, search stop
   * pickers) vs. the default standalone size used on a search result card. */
  compact?: boolean;
}

/**
 * The one visual marker every screen uses to tell "this is a live route,
 * ride it today" apart from "this is coverage data, browse only, never
 * live" — see CLAUDE.md's core principle for this stage: catalogue and live
 * must never be visually confusable. Live uses the app's existing
 * teal/"transit" tone (already the "currently moving" color everywhere
 * else); catalogue is deliberately muted/dashed, like faint pencil on a
 * noticeboard, never competing for attention with something actually
 * running.
 */
export function RouteSourceBadge({ source, compact = false }: RouteSourceBadgeProps) {
  const sizing = compact ? "px-1.5 py-0.5 text-[9px]" : "px-2 py-1 text-[10px]";

  if (source === "catalogue") {
    return (
      <span
        className={`inline-flex shrink-0 items-center gap-1 rounded-sm border border-dashed border-ink/40 bg-transparent font-display font-black uppercase tracking-wide text-ink/50 ${sizing}`}
      >
        Coverage
      </span>
    );
  }

  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded-sm border-2 border-transit bg-transit/95 font-display font-black uppercase tracking-wide text-dawn ${sizing}`}
    >
      Live
    </span>
  );
}
