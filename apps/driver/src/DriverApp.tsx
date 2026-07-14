import { useEffect, useState } from "react";
import type { AuthState } from "./context/AuthContext";
import { useAuth } from "./context/AuthContext";
import { useDriverSocket } from "./hooks/useDriverSocket";
import { useGeolocation, type Position } from "./hooks/useGeolocation";
import { api } from "./api/client";
import type { Route, SeatsResponse } from "./types";
import { BottomNav, type Tab } from "./components/BottomNav";
import { DashboardScreen } from "./screens/DashboardScreen";
import { ScanScreen } from "./screens/ScanScreen";
import { SeatsScreen } from "./screens/SeatsScreen";
import { AlertsScreen } from "./screens/AlertsScreen";

export function DriverApp({ auth }: { auth: AuthState }) {
  const { logout } = useAuth();
  const [tab, setTab] = useState<Tab>("home");

  const [routes, setRoutes] = useState<Route[]>([]);
  const [routesError, setRoutesError] = useState<string | null>(null);
  const [selectedRouteId, setSelectedRouteId] = useState<string | null>(null);
  const [wantOnline, setWantOnline] = useState(false);

  useEffect(() => {
    api
      .listRoutes()
      .then(setRoutes)
      .catch(() => setRoutesError("Could not load routes."));
  }, []);

  const socket = useDriverSocket(selectedRouteId, auth.token, wantOnline);

  const geolocation = useGeolocation(wantOnline, (pos: Position) => {
    socket.send({ lat: pos.lat, lng: pos.lng });
  });

  const [seats, setSeats] = useState<SeatsResponse | null>(null);
  const [seatsError, setSeatsError] = useState<string | null>(null);

  // Once the driver connection is up, pull the vehicle's current seat state
  // (delta: 0 is a no-op write that still returns the live state) so the
  // Seats screen has something to show immediately, not just after the
  // first manual adjustment.
  useEffect(() => {
    if (socket.status !== "open") return;
    api
      .updateSeats({ delta: 0 })
      .then((res) => {
        setSeats(res);
        setSeatsError(null);
      })
      .catch(() => setSeatsError("Could not load seat count."));
  }, [socket.status]);

  async function adjustSeats(next: { delta?: number; seats_available?: number }) {
    try {
      const res = await api.updateSeats(next);
      setSeats(res);
      setSeatsError(null);
    } catch {
      setSeatsError("Could not update seats — is your route still online?");
    }
  }

  function goOnline(routeId: string) {
    setSelectedRouteId(routeId);
    setWantOnline(true);
  }

  function goOffline() {
    setWantOnline(false);
    setSeats(null);
  }

  async function ackAlert(requestId: string) {
    await api.ackStopRequest(requestId);
    socket.dismissAlert(requestId);
  }

  return (
    <div className="min-h-screen bg-tar bg-grain pb-20 text-board">
      {tab === "home" && (
        <DashboardScreen
          routes={routes}
          routesError={routesError}
          selectedRouteId={selectedRouteId}
          wantOnline={wantOnline}
          socketStatus={socket.status}
          geoStatus={geolocation.status}
          lastPosition={geolocation.lastPosition}
          onGoOnline={goOnline}
          onGoOffline={goOffline}
          onLogout={logout}
        />
      )}
      {tab === "scan" && <ScanScreen />}
      {tab === "seats" && (
        <SeatsScreen
          online={socket.status === "open"}
          seats={seats}
          seatsError={seatsError}
          onAdjust={adjustSeats}
        />
      )}
      {tab === "alerts" && (
        <AlertsScreen alerts={socket.alerts} onAck={ackAlert} connected={socket.status === "open"} />
      )}

      <BottomNav active={tab} onChange={setTab} alertCount={socket.alerts.length} />
    </div>
  );
}
