/** Cents -> "R1,234.56". The only client-side transform applied to any
 * monetary figure in this app is unit conversion + display formatting — the
 * underlying number always comes straight from a Stage 8 /owner/* response,
 * never recomputed. See README.md's integrity note. */
export function formatRand(cents: number): string {
  const rands = cents / 100;
  return `R${rands.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("en-ZA", {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("en-ZA", { year: "numeric", month: "short", day: "2-digit" });
}
