import { useEffect, useRef, useState } from "react";
import { wsBaseUrl } from "../api/client";
import type { CommuterWSMessage, VehicleView } from "../types";

export type LiveStatus = "connecting" | "open" | "reconnecting" | "closed";

const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 8000];

/**
 * Owns the receive-only GET /ws/commuter?route_id=<id> connection: an
 * initial snapshot of currently-online vehicles, then incremental
 * update/offline events as they're published (Stage 4/6). Deliberately no
 * `enabled` gate the way the driver socket has — a commuter should be able
 * to watch a route the instant they pick one, logged in or not (the
 * endpoint itself is public).
 */
export function useRouteVehicles(routeId: string | null) {
  const [status, setStatus] = useState<LiveStatus>("closed");
  const [vehicles, setVehicles] = useState<Map<string, VehicleView>>(new Map());
  const socketRef = useRef<WebSocket | null>(null);
  const attemptRef = useRef(0);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const stoppedRef = useRef(false);

  useEffect(() => {
    setVehicles(new Map());

    if (!routeId) {
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

      const url = `${wsBaseUrl()}/ws/commuter?route_id=${encodeURIComponent(routeId!)}`;
      const socket = new WebSocket(url);
      socketRef.current = socket;

      socket.onopen = () => {
        attemptRef.current = 0;
        setStatus("open");
      };

      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data) as CommuterWSMessage;
          if (msg.type === "snapshot") {
            setVehicles(new Map(msg.vehicles.map((v) => [v.vehicle_id, v])));
          } else if (msg.type === "update") {
            setVehicles((prev) => {
              const next = new Map(prev);
              next.set(msg.vehicle.vehicle_id, msg.vehicle);
              return next;
            });
          } else if (msg.type === "offline") {
            setVehicles((prev) => {
              const next = new Map(prev);
              next.delete(msg.vehicle_id);
              return next;
            });
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
        setVehicles(new Map());
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
  }, [routeId]);

  return { status, vehicles: Array.from(vehicles.values()) };
}
