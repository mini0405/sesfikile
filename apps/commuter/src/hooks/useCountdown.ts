import { useEffect, useState } from "react";

export interface Countdown {
  remainingMs: number;
  expired: boolean;
  label: string;
}

/** Ticks once a second toward `expiresAt` (RFC3339). A short-TTL boarding
 * pass (Stage 5, ~3 minutes) needs a live, visibly counting-down clock, not a
 * static "expires at" timestamp. */
export function useCountdown(expiresAt: string | null): Countdown {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!expiresAt) return;
    const id = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(id);
  }, [expiresAt]);

  if (!expiresAt) return { remainingMs: 0, expired: true, label: "0:00" };

  const remainingMs = Math.max(0, new Date(expiresAt).getTime() - now);
  const totalSeconds = Math.ceil(remainingMs / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  const label = `${minutes}:${String(seconds).padStart(2, "0")}`;

  return { remainingMs, expired: remainingMs <= 0, label };
}
