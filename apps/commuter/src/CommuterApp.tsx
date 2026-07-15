import { useState } from "react";
import type { AuthState } from "./context/AuthContext";
import { useAuth } from "./context/AuthContext";
import { useRoutesData } from "./hooks/useRoutesData";
import { BottomNav, type Tab } from "./components/BottomNav";
import { MapScreen } from "./screens/MapScreen";
import { SearchScreen } from "./screens/SearchScreen";
import { RoutesScreen } from "./screens/RoutesScreen";
import { WalletScreen } from "./screens/WalletScreen";
import { BoardScreen } from "./screens/BoardScreen";

export function CommuterApp({ auth: _auth }: { auth: AuthState }) {
  const { logout } = useAuth();
  const [tab, setTab] = useState<Tab>("map");

  const { routes, stops, loading, error, catalogueLoaded, getRouteDetail } = useRoutesData();

  const [mapRouteId, setMapRouteId] = useState<string | null>(null);
  const [viewedRouteId, setViewedRouteId] = useState<string | null>(null);

  function viewRoute(routeId: string) {
    setViewedRouteId(routeId);
    setTab("routes");
  }

  return (
    <div className="min-h-screen bg-dawn bg-grain text-ink">
      {tab === "map" && (
        <MapScreen
          routes={routes}
          routesError={error}
          selectedRouteId={mapRouteId}
          onSelectRoute={setMapRouteId}
          onLogout={logout}
          catalogueLoaded={catalogueLoaded}
        />
      )}
      {tab === "search" && (
        <SearchScreen stops={stops} routes={routes} stopsLoading={loading} stopsError={error} onViewRoute={viewRoute} />
      )}
      {tab === "routes" && (
        <RoutesScreen
          routes={routes}
          loading={loading}
          error={error}
          selectedRouteId={viewedRouteId}
          onSelectRoute={setViewedRouteId}
          getRouteDetail={getRouteDetail}
        />
      )}
      {tab === "wallet" && <WalletScreen />}
      {tab === "board" && (
        <BoardScreen routes={routes} loading={loading} error={error} getRouteDetail={getRouteDetail} />
      )}

      <BottomNav active={tab} onChange={setTab} />
    </div>
  );
}
