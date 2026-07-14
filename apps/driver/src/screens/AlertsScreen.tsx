import { useState } from "react";
import type { StopRequestAlert } from "../types";
import { StatusPill } from "../components/StatusPill";

interface AlertsScreenProps {
  alerts: StopRequestAlert[];
  connected: boolean;
  onAck: (requestId: string) => Promise<void>;
}

export function AlertsScreen({ alerts, connected, onAck }: AlertsScreenProps) {
  const [ackingId, setAckingId] = useState<string | null>(null);
  const [ackedIds, setAckedIds] = useState<Set<string>>(new Set());

  async function handleAck(requestId: string) {
    setAckingId(requestId);
    try {
      await onAck(requestId);
      setAckedIds((prev) => new Set(prev).add(requestId));
    } finally {
      setAckingId(null);
    }
  }

  return (
    <div className="mx-auto max-w-md px-4 pt-6">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <p className="board-heading mb-1 text-tar-400">Dispatch</p>
          <h1 className="font-display text-2xl font-black uppercase tracking-tight text-board">Stop requests</h1>
        </div>
        <StatusPill label={connected ? "Listening" : "Off air"} tone={connected ? "green" : "slate"} />
      </div>

      {alerts.length === 0 && (
        <div className="rounded-sm border-2 border-dashed border-tar-600 p-8 text-center text-sm text-tar-400">
          No pickup calls right now. A call comes through here the instant a commuter ahead requests a stop.
        </div>
      )}

      <ul className="space-y-3">
        {alerts.map((alert) => {
          const acked = ackedIds.has(alert.request_id);
          return (
            <li key={alert.request_id} className="relative overflow-hidden rounded-sm border-2 border-brake/50 bg-tar-800 p-4">
              <span className="absolute left-0 top-0 h-full w-1.5 bg-brake" />
              <p className="pl-2 font-mono text-[11px] uppercase tracking-wide text-tar-400">
                {new Date(alert.requested_at).toLocaleTimeString()} · pickup call
              </p>
              <p className="mb-3 pl-2 font-display text-xl font-black uppercase tracking-tight text-board">
                {alert.stop_name}
              </p>
              <button
                onClick={() => void handleAck(alert.request_id)}
                disabled={acked || ackingId === alert.request_id}
                className={`ml-2 mr-2 rounded-sm border-2 px-4 py-2.5 text-sm font-black uppercase tracking-wide transition active:translate-y-px disabled:opacity-60 ${
                  acked ? "border-tar-600 text-tar-400" : "border-rank bg-rank/10 text-rank"
                }`}
                style={{ width: "calc(100% - 1rem)" }}
              >
                {acked ? "Acknowledged ✓" : ackingId === alert.request_id ? "Acknowledging…" : "Acknowledge"}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
