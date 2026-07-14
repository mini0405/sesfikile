import { useEffect, useRef, useState } from "react";

export interface Position {
  lat: number;
  lng: number;
}

export type GeoStatus = "idle" | "watching" | "denied" | "unsupported" | "error";

// Geolocation requires a "secure context" (https, or the special-cased
// http://localhost). It works out of the box in dev on localhost; testing
// on a real phone over plain http://<lan-ip> will fail permission checks
// silently in most browsers — see apps/driver/README.md.
export function useGeolocation(enabled: boolean, onUpdate: (pos: Position) => void) {
  const [status, setStatus] = useState<GeoStatus>("idle");
  const [lastPosition, setLastPosition] = useState<Position | null>(null);
  const onUpdateRef = useRef(onUpdate);
  onUpdateRef.current = onUpdate;

  useEffect(() => {
    if (!enabled) {
      setStatus("idle");
      return;
    }
    if (!("geolocation" in navigator)) {
      setStatus("unsupported");
      return;
    }

    setStatus("watching");
    const watchId = navigator.geolocation.watchPosition(
      (position) => {
        const next = { lat: position.coords.latitude, lng: position.coords.longitude };
        setLastPosition(next);
        onUpdateRef.current(next);
      },
      (err) => {
        setStatus(err.code === err.PERMISSION_DENIED ? "denied" : "error");
      },
      { enableHighAccuracy: true, maximumAge: 5000, timeout: 15000 },
    );

    return () => navigator.geolocation.clearWatch(watchId);
  }, [enabled]);

  return { status, lastPosition };
}
