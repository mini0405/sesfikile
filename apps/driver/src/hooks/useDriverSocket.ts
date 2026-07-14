import { useCallback, useEffect, useRef, useState } from "react";
import { wsBaseUrl } from "../api/client";
import type { StopRequestAlert } from "../types";

export type SocketStatus = "connecting" | "open" | "reconnecting" | "closed";

interface DriverMessage {
  lat?: number;
  lng?: number;
  seats_available?: number;
  seats_delta?: number;
  heartbeat?: boolean;
}

const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 8000];

/**
 * Owns the single bidirectional /ws/driver connection for the session:
 * driver -> server position/seat updates, server -> driver stop-request
 * alerts. Reconnects with backoff while `enabled` stays true (e.g. a dropped
 * connection while the driver is still toggled "online") rather than
 * silently giving up.
 */
export function useDriverSocket(routeId: string | null, token: string | null, enabled: boolean) {
  const [status, setStatus] = useState<SocketStatus>("closed");
  const [alerts, setAlerts] = useState<StopRequestAlert[]>([]);
  const socketRef = useRef<WebSocket | null>(null);
  const attemptRef = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stoppedRef = useRef(false);

  const send = useCallback((msg: DriverMessage) => {
    const socket = socketRef.current;
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify(msg));
    }
  }, []);

  const dismissAlert = useCallback((requestId: string) => {
    setAlerts((prev) => prev.filter((a) => a.request_id !== requestId));
  }, []);

  useEffect(() => {
    if (!enabled || !routeId || !token) {
      stoppedRef.current = true;
      socketRef.current?.close();
      socketRef.current = null;
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      setStatus("closed");
      return;
    }

    stoppedRef.current = false;
    attemptRef.current = 0;

    function connect() {
      if (stoppedRef.current) return;
      setStatus((prev) => (prev === "closed" ? "connecting" : "reconnecting"));

      const url = `${wsBaseUrl()}/ws/driver?route_id=${encodeURIComponent(routeId!)}&token=${encodeURIComponent(token!)}`;
      const socket = new WebSocket(url);
      socketRef.current = socket;

      socket.onopen = () => {
        attemptRef.current = 0;
        setStatus("open");
      };

      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          if (msg?.type === "stop_request") {
            setAlerts((prev) => [msg as StopRequestAlert, ...prev]);
          }
        } catch {
          // ignore malformed frames
        }
      };

      socket.onclose = () => {
        socketRef.current = null;
        if (stoppedRef.current) {
          setStatus("closed");
          return;
        }
        setStatus("reconnecting");
        const delay = RECONNECT_DELAYS_MS[Math.min(attemptRef.current, RECONNECT_DELAYS_MS.length - 1)];
        attemptRef.current += 1;
        reconnectTimer.current = setTimeout(connect, delay);
      };

      socket.onerror = () => {
        socket.close();
      };
    }

    connect();

    return () => {
      stoppedRef.current = true;
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      socketRef.current?.close();
      socketRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [routeId, token, enabled]);

  return { status, alerts, send, dismissAlert };
}
