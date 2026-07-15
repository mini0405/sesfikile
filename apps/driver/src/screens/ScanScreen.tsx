import { useEffect, useRef, useState } from "react";
import { Html5Qrcode } from "html5-qrcode";
import { api, ApiError } from "../api/client";
import type { ScanPassResponse } from "../types";

const READER_ELEMENT_ID = "qr-reader";

type Mode = "idle" | "camera" | "manual" | "result";

export function ScanScreen() {
  const [mode, setMode] = useState<Mode>("idle");
  const [manualCode, setManualCode] = useState("");
  const [manualToken, setManualToken] = useState("");
  const [showTokenFallback, setShowTokenFallback] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ScanPassResponse | null>(null);
  const scannerRef = useRef<Html5Qrcode | null>(null);

  useEffect(() => {
    if (mode !== "camera") return;

    let cancelled = false;
    const scanner = new Html5Qrcode(READER_ELEMENT_ID);
    scannerRef.current = scanner;

    scanner
      .start(
        { facingMode: "environment" },
        { fps: 10, qrbox: { width: 250, height: 250 } },
        (decodedText) => {
          if (cancelled) return;
          cancelled = true;
          void handleDecoded(decodedText);
        },
        () => {
          // per-frame "no QR found" callback — expected on most frames, ignore
        },
      )
      .catch(() => {
        if (!cancelled) setError("Could not access the camera. Check permissions, or paste the token instead.");
      });

    return () => {
      cancelled = true;
      scanner.stop().catch(() => {});
      scanner.clear();
      scannerRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  async function stopCamera() {
    const scanner = scannerRef.current;
    if (scanner) {
      try {
        await scanner.stop();
      } catch {
        // already stopped
      }
    }
  }

  // The QR now encodes only the short code (Stage 5 upgrade: airport-style
  // boarding codes) — a raw signed pass_token contains a "." separating its
  // payload/signature segments, which an 8-character Crockford-base32 code
  // never does, so this dispatches on shape rather than needing two camera
  // modes. Kept for dev/back-compat: a pasted or scanned full token still works.
  async function handleDecoded(text: string) {
    const trimmed = text.trim();
    if (trimmed.includes(".")) {
      await chargeByToken(trimmed);
    } else {
      await chargeByCode(trimmed);
    }
  }

  async function chargeByCode(code: string) {
    await stopCamera();
    setBusy(true);
    setError(null);
    try {
      const res = await api.scanBoardingCode(code);
      setResult(res);
      setMode("result");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Scan failed. Try again.");
      setMode("idle");
    } finally {
      setBusy(false);
    }
  }

  async function chargeByToken(token: string) {
    await stopCamera();
    setBusy(true);
    setError(null);
    try {
      const res = await api.scanBoardingPass(token);
      setResult(res);
      setMode("result");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Scan failed. Try again.");
      setMode("idle");
    } finally {
      setBusy(false);
    }
  }

  function reset() {
    setResult(null);
    setError(null);
    setManualCode("");
    setManualToken("");
    setShowTokenFallback(false);
    setMode("idle");
  }

  return (
    <div className="mx-auto max-w-md px-4 pt-6">
      <p className="board-heading mb-1 text-tar-400">Fare on scan</p>
      <h1 className="mb-4 font-display text-2xl font-black uppercase tracking-tight text-board">
        Scan boarding pass
      </h1>

      {error && (
        <div className="mb-4 rounded-sm border-2 border-brake bg-brake/10 px-3 py-2 text-sm font-medium text-brake">
          {error}
        </div>
      )}

      {mode === "idle" && (
        <div className="space-y-3">
          <button
            onClick={() => setMode("camera")}
            className="flex w-full items-center justify-center gap-2 rounded-sm border-2 border-ink bg-rank py-6 font-display text-lg font-black uppercase tracking-wide text-ink shadow-tape transition active:translate-y-px"
          >
            📷 Open camera
          </button>
          <button onClick={() => setMode("manual")} className="btn-ghost">
            Enter boarding code instead
          </button>
        </div>
      )}

      {mode === "camera" && (
        <div className="space-y-3">
          <div className="relative overflow-hidden rounded-sm border-2 border-rank">
            <div id={READER_ELEMENT_ID} className="bg-tar-800" />
            {/* Viewfinder corner brackets — a rank-sign frame, not a generic dashed box. */}
            <div className="pointer-events-none absolute left-2 top-2 h-6 w-6 border-l-4 border-t-4 border-rank" />
            <div className="pointer-events-none absolute right-2 top-2 h-6 w-6 border-r-4 border-t-4 border-rank" />
            <div className="pointer-events-none absolute bottom-2 left-2 h-6 w-6 border-b-4 border-l-4 border-rank" />
            <div className="pointer-events-none absolute bottom-2 right-2 h-6 w-6 border-b-4 border-r-4 border-rank" />
          </div>
          <p className="text-center text-sm text-tar-400">Point the camera at the commuter&rsquo;s QR code.</p>
          <button onClick={() => setMode("idle")} className="btn-ghost">
            Cancel
          </button>
        </div>
      )}

      {mode === "manual" && (
        <div className="space-y-4">
          {/* The primary fallback when the camera is unavailable — e.g. no
              secure-context camera permission over plain http on a LAN phone
              demo. Big, uppercase, code-friendly: matches the commuter app's
              grouped K7M2-9XQP display, case-insensitive and hyphen-tolerant
              on submit (the backend normalizes it too, but doing it here
              keeps what the driver sees honest about what will be sent). */}
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void chargeByCode(manualCode);
            }}
            className="space-y-3"
          >
            <label className="block text-xs font-bold uppercase tracking-wide text-tar-400">Boarding code</label>
            <input
              type="text"
              inputMode="text"
              autoCapitalize="characters"
              autoCorrect="off"
              spellCheck={false}
              value={manualCode}
              onChange={(e) => setManualCode(e.target.value.toUpperCase())}
              placeholder="K7M2-9XQP"
              className="w-full rounded-sm border-2 border-tar-600 bg-tar-800 px-3 py-4 text-center font-display text-3xl font-black uppercase tracking-[0.15em] text-board outline-none focus:border-rank"
            />
            <button type="submit" disabled={busy || manualCode.trim().length === 0} className="btn-rank">
              {busy ? "Charging…" : "Charge fare"}
            </button>
            <button type="button" onClick={() => setMode("idle")} className="btn-ghost">
              Cancel
            </button>
          </form>

          <details
            className="text-left"
            open={showTokenFallback}
            onToggle={(e) => setShowTokenFallback(e.currentTarget.open)}
          >
            <summary className="cursor-pointer text-xs font-bold uppercase tracking-wide text-tar-400">
              Dev fallback: paste full pass token
            </summary>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void chargeByToken(manualToken);
              }}
              className="mt-2 space-y-3"
            >
              <textarea
                value={manualToken}
                onChange={(e) => setManualToken(e.target.value)}
                placeholder="Paste the pass_token string here"
                rows={4}
                className="w-full rounded-sm border-2 border-tar-600 bg-tar-800 px-3 py-2.5 font-mono text-xs text-board outline-none focus:border-rank"
              />
              <button type="submit" disabled={busy || manualToken.trim().length === 0} className="btn-ghost">
                {busy ? "Charging…" : "Charge with raw token"}
              </button>
            </form>
          </details>
        </div>
      )}

      {mode === "result" && result && (
        <div className="space-y-4">
          {/* The fare slip — a torn ticket stub with a rubber-stamp verdict. */}
          <div className="ticket text-center">
            <span className={`stamp mb-3 ${result.replayed ? "stamp-replay" : "stamp-paid"}`}>
              {result.replayed ? "Already Paid" : "Paid"}
            </span>
            <p className="board-heading mb-1">Fare charged</p>
            <p className="font-mono text-4xl font-bold tabular-nums">R{(result.fare_cents / 100).toFixed(2)}</p>
            {result.replayed && (
              <p className="mt-2 text-xs font-medium text-brake">
                This pass was already scanned — the commuter was not charged again.
              </p>
            )}

            <div className="mt-4 border-t-2 border-dashed border-ink/20 pt-4 text-left text-sm">
              <Row label="Driver share" value={`R${(result.driver_cents / 100).toFixed(2)}`} />
              <Row label="Owner share" value={`R${(result.owner_cents / 100).toFixed(2)}`} />
              <Row label="Platform fee" value={`R${(result.platform_cents / 100).toFixed(2)}`} />
              <Row label="Seats remaining" value={String(result.seats_remaining)} last />
            </div>
          </div>

          <button onClick={reset} className="btn-rank">
            Scan next pass
          </button>
        </div>
      )}
    </div>
  );
}

function Row({ label, value, last }: { label: string; value: string; last?: boolean }) {
  return (
    <div className={`flex items-center justify-between py-1.5 ${last ? "" : "border-b border-ink/10"}`}>
      <span className="text-ink/60">{label}</span>
      <span className="font-mono font-bold tabular-nums text-ink">{value}</span>
    </div>
  );
}
