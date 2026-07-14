import { useEffect, useState } from "react";
import { api, ApiError } from "../api/client";

const PRESET_AMOUNTS_CENTS = [2000, 5000, 10000];

interface LocalTopup {
  id: string;
  amountCents: number;
  newBalanceCents: number;
  at: number;
}

export function WalletScreen() {
  const [balanceCents, setBalanceCents] = useState<number | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyAmount, setBusyAmount] = useState<number | null>(null);
  const [topupError, setTopupError] = useState<string | null>(null);
  const [customRand, setCustomRand] = useState("");
  const [history, setHistory] = useState<LocalTopup[]>([]);

  async function loadBalance() {
    setLoading(true);
    setLoadError(null);
    try {
      const res = await api.getBalance();
      setBalanceCents(res.balance_cents);
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : "Could not load your wallet balance.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadBalance();
  }, []);

  async function doTopup(amountCents: number) {
    setTopupError(null);
    setBusyAmount(amountCents);
    try {
      const res = await api.topup(amountCents);
      setBalanceCents(res.balance_cents);
      setHistory((prev) => [
        { id: res.transaction_id, amountCents, newBalanceCents: res.balance_cents, at: Date.now() },
        ...prev,
      ]);
      setCustomRand("");
    } catch (err) {
      setTopupError(err instanceof ApiError ? err.message : "Top-up failed. Try again.");
    } finally {
      setBusyAmount(null);
    }
  }

  function handleCustomTopup() {
    const rand = Number(customRand);
    if (!Number.isFinite(rand) || rand <= 0) {
      setTopupError("Enter a positive Rand amount.");
      return;
    }
    void doTopup(Math.round(rand * 100));
  }

  return (
    <div className="mx-auto max-w-md px-4 pb-20 pt-6">
      <div className="mb-3 flex items-center justify-between">
        <p className="board-heading text-dawn-400">Ses&rsquo;fikile · Wallet</p>
        <button
          onClick={() => void loadBalance()}
          className="text-xs font-bold uppercase tracking-wide text-dawn-400 active:text-marigold-700"
        >
          Refresh
        </button>
      </div>

      <div className="board relative mb-4 px-4 pb-5 pt-6 text-center">
        <span className="tape left-6" />
        <span className="tape right-8 rotate-[5deg] bg-transit/70" />

        <p className="board-heading mb-2">Balance</p>
        {loadError ? (
          <p className="text-sm font-medium text-flag">{loadError}</p>
        ) : (
          <p className="font-display text-5xl font-black tabular-nums text-ink">
            {loading && balanceCents === null ? "…" : `R${((balanceCents ?? 0) / 100).toFixed(2)}`}
          </p>
        )}
      </div>

      <div className="board relative mb-4 px-4 pb-4 pt-6">
        <span className="tape left-6" />

        <p className="board-heading mb-1">Top up</p>
        <p className="mb-3 text-xs text-ink/60">
          Demo top-up only — this simulates a load onto your wallet. There is no real payment gateway in this MVP.
        </p>

        <div className="mb-3 grid grid-cols-3 gap-2">
          {PRESET_AMOUNTS_CENTS.map((cents) => (
            <button
              key={cents}
              onClick={() => void doTopup(cents)}
              disabled={busyAmount !== null}
              className="rounded-sm border-2 border-ink bg-marigold px-2 py-3 text-center font-display text-sm font-black text-ink shadow-tape transition active:translate-y-px disabled:opacity-50"
            >
              {busyAmount === cents ? "…" : `R${(cents / 100).toFixed(0)}`}
            </button>
          ))}
        </div>

        <div className="flex gap-2">
          <input
            type="number"
            min={1}
            step="0.01"
            inputMode="decimal"
            placeholder="Custom amount (R)"
            value={customRand}
            onChange={(e) => setCustomRand(e.target.value)}
            className="w-full rounded-sm border-2 border-ink/70 bg-board-dim px-3 py-2.5 text-sm font-bold text-ink outline-none focus:border-transit"
          />
          <button
            onClick={handleCustomTopup}
            disabled={busyAmount !== null || customRand.trim() === ""}
            className="shrink-0 rounded-sm border-2 border-ink bg-marigold px-4 py-2.5 font-display text-xs font-black uppercase text-ink shadow-tape transition active:translate-y-px disabled:opacity-50"
          >
            {busyAmount !== null && String(busyAmount) === String(Math.round(Number(customRand) * 100))
              ? "…"
              : "Load"}
          </button>
        </div>

        {topupError && (
          <div className="mt-3 rounded-sm border-2 border-flag bg-flag/10 px-3 py-2 text-sm font-medium text-flag">
            {topupError}
          </div>
        )}
      </div>

      <div className="board relative px-4 pb-4 pt-6">
        <span className="tape right-8 rotate-[-4deg] bg-marigold/70" />

        <p className="board-heading mb-2">Recent top-ups (this session)</p>
        {history.length === 0 ? (
          <p className="text-sm text-ink/60">
            No top-ups yet this session. Full ledger history isn&rsquo;t exposed to commuters in this MVP — only the
            owner dashboard (Stage 8) can see the full ledger.
          </p>
        ) : (
          <div className="space-y-1 border-t border-dashed border-ink/30 pt-3">
            {history.map((h) => (
              <div key={h.id} className="flex items-center justify-between text-sm">
                <span className="text-ink/70">{new Date(h.at).toLocaleTimeString()} · Demo top-up</span>
                <span className="font-mono font-bold tabular-nums text-ink">+R{(h.amountCents / 100).toFixed(2)}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
